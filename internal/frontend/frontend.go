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

// Package frontend is what users run. It drives `go vet` with gomemsafe as the vet tool,
// reads the JSON diagnostics, renders them in a Rust-like format, and decides the exit code.
//
// It always asks vet for JSON. In JSON mode the vet tool exits 0 even when it has findings,
// and the go command caches every run that exits 0, so re-running on a tree with findings is
// a cache hit. The front-end therefore decides the exit code itself: 0 clean, 1 findings, 2
// errors, where "errors" includes analyzer failures reported inside the JSON. Test variants
// of a package repeat its diagnostics, so duplicates are dropped.
//
// The files split by concern: frontend.go runs vet and decodes its output, render.go prints
// diagnostics with source snippets, and stats.go summarises GOMEMSAFE_TRACE events for
// -stats.
package frontend

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/tochemey/gomemsafe/internal/trace"
)

// ProgramName prefixes every message the front-end prints.
const ProgramName = "gomemsafe"

// Exit codes of the front-end.
const (
	ExitClean    = 0
	ExitFindings = 1
	ExitError    = 2
)

const usage = `usage: gomemsafe [-json] [-stats] [analyzer flags] [packages]

  -json    print vet's JSON output unchanged
  -stats   print where the time went (forces a fresh run of every package)

Analyzer flags are passed to go vet unchanged, e.g. -lifetime or -lifetime=false.
Packages default to ./...
`

// The go command line the front-end runs: go vet -vettool=<self> -json [-a] [flags] [patterns].
const (
	goCommand      = "go"
	vetSubcommand  = "vet"
	vetToolFlag    = "-vettool="
	jsonFlag       = "-json"
	rebuildAllFlag = "-a"
	defaultPattern = "./..."
)

// Front-end flags. Both the single- and double-dash spellings are accepted, as the flag
// package does.
const (
	statsFlag = "-stats"
	helpFlag  = "-h"
	helpLong  = "-help"
)

// executable returns the path of the vet tool to run. Tests point it at a built binary.
var executable = os.Executable

var errHelp = errors.New("help requested")

// options is the parsed command line.
type options struct {
	json, stats bool
	vetFlags    []string
	patterns    []string
}

// Diagnostic is one finding, in the JSON form go vet -json prints.
type Diagnostic struct {
	Category       string `json:"category"`
	Posn           string `json:"posn"`
	Message        string `json:"message"`
	SuggestedFixes []struct {
		Message string `json:"message"`
	} `json:"suggested_fixes"`
	Related []struct {
		Posn    string `json:"posn"`
		Message string `json:"message"`
	} `json:"related"`
}

// results is everything one go vet run reported.
type results struct {
	diagnostics []Diagnostic
	errors      []string
}

// printer writes formatted output and keeps the first write error, so a run of writes is
// checked once, the way bufio.Writer reports errors.
type printer struct {
	w   io.Writer
	err error
}

// Decode parses the JSON go vet -json prints into its diagnostics, sorted by position and
// without the duplicates that test variants of a package produce, and its analyzer errors.
func Decode(data []byte) ([]Diagnostic, []string, error) {
	r, err := decode(data)
	return r.diagnostics, r.errors, err
}

// Main runs the front-end and returns the exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	out := &printer{w: stdout}
	diag := &printer{w: stderr}

	code := run(args, out, diag)
	if out.err != nil || diag.err != nil {
		return ExitError
	}

	return code
}

func run(args []string, out, diag *printer) int {
	opts, err := parseArgs(args)
	if err != nil {
		if !errors.Is(err, errHelp) {
			diag.printf(ProgramName+": %v\n", err)
		}

		diag.printf("%s", usage)

		return ExitError
	}

	self, err := executable()
	if err != nil {
		diag.printf(ProgramName+": %v\n", err)
		return ExitError
	}

	var traceDir string

	if opts.stats {
		if traceDir, err = os.MkdirTemp("", "gomemsafe-stats-"); err != nil {
			diag.printf(ProgramName+": %v\n", err)
			return ExitError
		}

		defer func() {
			if err := os.RemoveAll(traceDir); err != nil {
				diag.printf(ProgramName+": removing %s: %v\n", traceDir, err)
			}
		}()
	}

	output, err := runVet(self, opts, traceDir, diag)
	if err != nil {
		diag.printf(ProgramName+": %v\n", err)
		return ExitError
	}

	res, err := decode(output)
	if err != nil {
		diag.printf(ProgramName+": reading go vet output: %v\n", err)
		return ExitError
	}

	if opts.json {
		out.write(output)
	} else {
		render(out, res.diagnostics)
	}

	for _, e := range res.errors {
		diag.printf(ProgramName+": %s\n", e)
	}

	if opts.stats {
		if err := printStats(diag, traceDir); err != nil {
			diag.printf(ProgramName+": stats: %v\n", err)
		}
	}

	return exitCode(res)
}

func exitCode(res results) int {
	switch {
	case len(res.errors) > 0:
		return ExitError
	case len(res.diagnostics) > 0:
		return ExitFindings
	}

	return ExitClean
}

func parseArgs(args []string) (options, error) {
	var o options

	for i, a := range args {
		if !strings.HasPrefix(a, "-") {
			o.patterns = args[i:]
			break
		}

		switch a {
		case jsonFlag, "-" + jsonFlag:
			o.json = true
		case statsFlag, "-" + statsFlag:
			o.stats = true
		case helpFlag, helpLong, "-" + helpLong:
			return o, errHelp
		default:
			o.vetFlags = append(o.vetFlags, a)
		}
	}

	if len(o.patterns) == 0 {
		o.patterns = []string{defaultPattern}
	}

	return o, nil
}

// runVet runs go vet with self as the vet tool and returns its JSON output. Build failures
// and type errors make go vet itself fail. Vet's stderr is always passed on: on success it
// holds only the go command's own notices, such as a pattern that matched no packages.
func runVet(self string, o options, traceDir string, diag *printer) ([]byte, error) {
	args := []string{vetSubcommand, vetToolFlag + self, jsonFlag}
	if traceDir != "" {
		// A fresh run of every package: cached packages would leave nothing to measure. It goes
		// before the user's flags, which may be followed by their values.
		args = append(args, rebuildAllFlag)
	}

	args = append(args, o.vetFlags...)
	args = append(args, o.patterns...)

	var stdout, stderr bytes.Buffer

	// The arguments are the user's own flags and package patterns, passed to the go command
	// exactly as `go vet` would receive them; running them is the front-end's purpose.
	cmd := exec.Command(goCommand, args...) //nolint:gosec // G204: see the comment above.
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	cmd.Env = os.Environ()
	if traceDir != "" {
		cmd.Env = append(cmd.Env, trace.EnvVar+"="+traceDir)
	}

	err := cmd.Run()

	diag.write(stderr.Bytes())

	if err != nil {
		return nil, fmt.Errorf("go vet failed: %w", err)
	}

	return stdout.Bytes(), nil
}

// decode reads the stream of JSON trees go vet prints, one per package:
// {"<package ID>": {"<analyzer>": [diagnostics] | {"error": "..."}}}.
// Test variants of a package report the same diagnostics again, so duplicates are dropped.
func decode(data []byte) (results, error) {
	var r results

	seen := make(map[string]bool)
	dec := json.NewDecoder(bytes.NewReader(data))

	for {
		var tree map[string]map[string]json.RawMessage

		err := dec.Decode(&tree)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return r, err
		}

		for pkg, byAnalyzer := range tree {
			for name, raw := range byAnalyzer {
				if err := r.add(pkg, name, raw, seen); err != nil {
					return r, err
				}
			}
		}
	}

	sort.Slice(r.diagnostics, func(i, j int) bool {
		return lessPosn(r.diagnostics[i].Posn, r.diagnostics[j].Posn)
	})
	sort.Strings(r.errors)

	return r, nil
}

// add records one analyzer's output for one package: an error object or a diagnostic list.
func (r *results) add(pkg, analyzer string, raw json.RawMessage, seen map[string]bool) error {
	var failure struct {
		Err string `json:"error"`
	}

	if json.Unmarshal(raw, &failure) == nil && failure.Err != "" {
		r.errors = append(r.errors, fmt.Sprintf("%s: %s: %s", pkg, analyzer, failure.Err))
		return nil
	}

	var ds []Diagnostic
	if err := json.Unmarshal(raw, &ds); err != nil {
		return fmt.Errorf("%s: %s: %w", pkg, analyzer, err)
	}

	for _, d := range ds {
		key := d.Posn + "\x00" + d.Message
		if !seen[key] {
			seen[key] = true

			r.diagnostics = append(r.diagnostics, d)
		}
	}

	return nil
}

func (p *printer) printf(format string, args ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, args...)
	}
}

func (p *printer) write(b []byte) {
	if p.err == nil {
		_, p.err = p.w.Write(b)
	}
}
