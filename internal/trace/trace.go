// MIT License
//
// Copyright (c) 2026 Arsene Gandote
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

// Package trace records where vet-tool processes spend their time, for benchmarking and for
// the front-end's -stats flag. When GOMEMSAFE_TRACE names a directory, each process appends
// one JSON Event per line to <dir>/<pid>.jsonl: one per analyzer run (Wrap), per SSA build,
// per function abandoned at the dataflow budget, and per check skipped in a facts-only run.
// When the variable is unset, every function here returns after one comparison.
//
// The go command's vet cache key does not include environment variables, so traces are only
// meaningful with a fresh GOCACHE (or go vet -a): cached packages run nothing and record
// nothing.
package trace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/tools/go/analysis"

	"github.com/tochemey/gomemsafe/internal/vetmode"
)

// EnvVar names the trace directory.
const EnvVar = "GOMEMSAFE_TRACE"

// CompileRSSEnvVar names the file the benchmark's -toolexec wrapper (internal/cmd/rsswrap)
// appends each compile's peak RSS to.
const CompileRSSEnvVar = "GOMEMSAFE_COMPILE_RSS"

// FileExt is the extension of trace files: one JSON event per line, one file per process.
const FileExt = ".jsonl"

// Event kinds.
const (
	// KindRun is an analyzer's Run.
	KindRun = "run"
	// KindSSA is the construction of a package's SSA form.
	KindSSA = "ssa"
	// KindBudget is a function abandoned at the dataflow budget.
	KindBudget = "budget"
	// KindSkipped is a check that returned immediately because the run only computes facts.
	KindSkipped = "skipped"
)

var (
	dir = os.Getenv(EnvVar)
	// start approximates the process start: package variables are initialised before main.
	start    = time.Now()
	mu       sync.Mutex
	file     *os.File
	openErr  error
	openOnce sync.Once
)

// Event is one trace line.
type Event struct {
	PID      int    `json:"pid"`
	Package  string `json:"package"`
	Analyzer string `json:"analyzer"`
	// Kind is one of KindRun, KindSSA, KindBudget, or KindSkipped.
	Kind     string `json:"kind"`
	VetxOnly bool   `json:"vetx_only"`
	Nanos    int64  `json:"nanos"`
	MaxRSS   int64  `json:"max_rss"`
	// Elapsed is the time since the process started, so the last event of a process gives its
	// wall time, type checking included.
	Elapsed int64 `json:"elapsed_nanos"`
}

// Enabled reports whether tracing is on.
func Enabled() bool { return dir != "" }

// Record appends an event for pkg. It never fails the analysis: trace errors are reported once
// on stderr.
func Record(pkg, analyzer, kind string, d time.Duration) {
	if dir == "" {
		return
	}

	openOnce.Do(func() {
		file, openErr = os.OpenFile(filepath.Clean(filepath.Join(dir, fmt.Sprintf("%d%s", os.Getpid(), FileExt))),
			os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if openErr != nil {
			fmt.Fprintln(os.Stderr, "gomemsafe: trace disabled:", openErr)
		}
	})

	if openErr != nil {
		return
	}

	line, err := json.Marshal(Event{
		PID: os.Getpid(), Package: pkg, Analyzer: analyzer, Kind: kind,
		VetxOnly: vetmode.FactsOnly(), Nanos: d.Nanoseconds(), MaxRSS: maxRSS(),
		Elapsed: time.Since(start).Nanoseconds(),
	})
	if err != nil {
		return
	}

	mu.Lock()
	defer mu.Unlock()

	_, _ = file.Write(append(line, '\n'))
}

// Wrap makes every analyzer reachable from roots record the duration of its Run. It must be
// called before the analyzers run, and only when tracing is enabled.
func Wrap(roots []*analysis.Analyzer) {
	seen := make(map[*analysis.Analyzer]bool)

	var walk func(a *analysis.Analyzer)

	walk = func(a *analysis.Analyzer) {
		if seen[a] {
			return
		}

		seen[a] = true
		for _, r := range a.Requires {
			walk(r)
		}

		run, name := a.Run, a.Name
		a.Run = func(pass *analysis.Pass) (any, error) {
			start := time.Now()
			res, err := run(pass)
			Record(pass.Pkg.Path(), name, KindRun, time.Since(start))

			return res, err
		}
	}
	for _, a := range roots {
		walk(a)
	}
}
