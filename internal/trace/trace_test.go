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

package trace

import (
	"bufio"
	"encoding/json"
	"go/types"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/tools/go/analysis"
)

func TestRecordAndWrapWriteEvents(t *testing.T) {
	dir = t.TempDir()
	t.Cleanup(func() { dir = "" })

	if !Enabled() {
		t.Fatal("Enabled() = false with a trace directory set")
	}

	dep := &analysis.Analyzer{Name: "dep", Doc: "dep", Run: func(*analysis.Pass) (any, error) { return 1, nil }}
	root := &analysis.Analyzer{
		Name: "root", Doc: "root", Requires: []*analysis.Analyzer{dep},
		Run: func(*analysis.Pass) (any, error) { return 2, nil },
	}
	Wrap([]*analysis.Analyzer{root, root})

	pass := &analysis.Pass{Pkg: types.NewPackage("example.com/p", "p")}

	if res, err := root.Run(pass); err != nil || res != 2 {
		t.Fatalf("wrapped Run = %v, %v; want the original result", res, err)
	}

	if res, err := dep.Run(pass); err != nil || res != 1 {
		t.Fatalf("wrapped dependency Run = %v, %v; want the original result", res, err)
	}

	Record("example.com/p", "lazyssa", KindSSA, time.Millisecond)

	events := readEvents(t)
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}

	if e := events[0]; e.Analyzer != "root" || e.Kind != KindRun || e.Package != "example.com/p" || e.PID != os.Getpid() {
		t.Fatalf("first event = %+v", e)
	}

	if e := events[2]; e.Kind != KindSSA || e.Nanos != int64(time.Millisecond) {
		t.Fatalf("last event = %+v", e)
	}
}

func TestRecordIsANoOpWhenDisabled(t *testing.T) {
	dir = ""

	if Enabled() {
		t.Fatal("Enabled() = true without a trace directory")
	}

	Record("p", "a", KindRun, time.Second)
}

// BenchmarkRecordDisabled is the cost tracing adds to every analyzer run when it is off.
func BenchmarkRecordDisabled(b *testing.B) {
	dir = ""

	for b.Loop() {
		Record("p", "a", KindRun, time.Second)
	}
}

func readEvents(t *testing.T) []Event {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("trace files = %v, %v; want exactly one", files, err)
	}

	f, err := os.Open(files[0])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	var events []Event

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatal(err)
		}

		events = append(events, e)
	}

	return events
}
