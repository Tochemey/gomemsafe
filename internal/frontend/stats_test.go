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

package frontend

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tochemey/gomemsafe/internal/trace"
)

func TestPrintStatsSummarisesEvents(t *testing.T) {
	dir := t.TempDir()
	writeEvents(t, filepath.Join(dir, "1.jsonl"),
		trace.Event{Package: "p", Analyzer: "lifetime", Kind: trace.KindRun, Nanos: 8e6, MaxRSS: 2 << 20},
		trace.Event{Package: "p", Analyzer: "lazyssa", Kind: trace.KindSSA, Nanos: 2e6},
		trace.Event{Package: "q", Analyzer: "lifetime", Kind: trace.KindBudget},
	)

	var out bytes.Buffer
	if err := printStats(&printer{w: &out}, dir); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"2 packages, 8ms in analyzers, peak RSS 2.0 MiB",
		"SSA construction: 2ms (25.0% of analyzer time)",
		"dataflow budget hits: 1",
		"lifetime     8ms",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("stats lack %q:\n%s", want, out.String())
		}
	}
}

func TestPrintStatsRejectsMalformedTraces(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "1.jsonl"), []byte("{not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := printStats(&printer{w: &bytes.Buffer{}}, dir); err == nil {
		t.Fatal("no error for a malformed trace line")
	}
}

func TestReadStatsOfEmptyDirectory(t *testing.T) {
	s, err := readStats(t.TempDir())
	if err != nil || len(s.packages) != 0 || s.total != 0 {
		t.Fatalf("readStats = %+v, %v; want empty", s, err)
	}
}

func TestReadStatsFailures(t *testing.T) {
	if _, err := readStats("["); err == nil {
		t.Error("a directory that is not a valid pattern: no error")
	}

	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "1"+trace.FileExt)); err != nil {
		t.Fatal(err)
	}

	if _, err := readStats(dir); err == nil {
		t.Error("a trace file that cannot be opened: no error")
	}
}

func writeEvents(t *testing.T, name string, events ...trace.Event) {
	t.Helper()

	var buf bytes.Buffer

	for _, e := range events {
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}

		buf.Write(append(line, '\n'))
	}

	if err := os.WriteFile(name, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}
