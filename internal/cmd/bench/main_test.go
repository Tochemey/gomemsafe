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

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cancelOnWrite is a stderr that cancels the run when it announces an entry, counting the
// announcements made while the run's work directory exists in dir.
type cancelOnWrite struct {
	cancel  context.CancelFunc
	dir     string
	entries int
}

func TestParseOptions(t *testing.T) {
	var stderr bytes.Buffer

	o, err := parseOptions([]string{"-short", "-corpus", "zap,synthetic", "-scenario", "cold,rss", "-out", "x"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}

	if !o.short || len(o.names) != 2 || !o.scenarios[scenarioCold] || !o.scenarios[scenarioRSS] ||
		o.scenarios[scenarioWarm] || o.outDir != "x" {
		t.Fatalf("options = %+v", o)
	}

	if _, err := parseOptions([]string{"-scenario", "nope"}, &stderr); err == nil {
		t.Fatal("unknown scenario: no error")
	}
}

func TestRunBenchExitCodes(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if code := runBench([]string{"-h"}, &stdout, &stderr); code != 2 {
		t.Fatalf("-h: exit %d, want 2", code)
	}

	if code := runBench([]string{"-corpus-file", filepath.Join(t.TempDir(), "missing.json")}, &stdout, &stderr); code != 1 {
		t.Fatalf("missing corpus: exit %d, want 1", code)
	}

	if code := runBench([]string{"-corpus", "nope"}, &stdout, &stderr); code != 1 {
		t.Fatalf("unknown entry: exit %d, want 1", code)
	}
}

func TestBenchmarkRunsEveryScenarioAndLeavesNothingBehind(t *testing.T) {
	tmp := isolateTempDir(t)
	corpus := filepath.Join(t.TempDir(), corpusFileName)
	write(t, corpus, `[{"name": "synthetic", "synthetic": true, "small": true, "patterns": ["./..."]}]`)

	var stdout, stderr bytes.Buffer

	if code := runBench([]string{"-corpus-file", corpus}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr.String())
	}

	row := ""

	for line := range strings.Lines(stdout.String()) {
		if strings.HasPrefix(line, "| synthetic |") {
			row = line
		}
	}

	if row == "" || strings.Contains(row, verdictNA) {
		t.Fatalf("want every scenario measured, got row %q in:\n%s", row, stdout.String())
	}

	assertEmpty(t, tmp)
}

func TestBenchmarkCleansUpWhenCancelled(t *testing.T) {
	tmp := isolateTempDir(t)
	corpus := filepath.Join(t.TempDir(), corpusFileName)
	write(t, corpus, `[{"name": "synthetic", "synthetic": true, "small": true, "patterns": ["./..."]}]`)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel once the run announces its first entry: by then it has created its work directory
	// and built its tools, so the cleanup has something to remove.
	var stdout bytes.Buffer

	stderr := &cancelOnWrite{cancel: cancel, dir: tmp}

	o := options{corpusFile: corpus, scenarios: map[string]bool{scenarioCold: true}}
	if err := benchmark(ctx, o, &stdout, stderr); err == nil {
		t.Fatal("a cancelled run reported success")
	}

	if stderr.entries == 0 {
		t.Fatal("the run was cancelled before it created its work directory")
	}

	assertEmpty(t, tmp)
}

func TestPrepareSourceGeneratesTheSyntheticCorpus(t *testing.T) {
	dir, err := prepareSource(context.Background(), Entry{Name: syntheticEntry, Synthetic: true, Small: true}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, goModFile)); err != nil {
		t.Fatalf("synthetic: %v", err)
	}
}

func TestCloneAtPinsTheCommit(t *testing.T) {
	ctx := context.Background()
	origin := t.TempDir()

	for _, args := range [][]string{
		{"init", "--quiet"},
		{"-c", "user.name=bench", "-c", "user.email=bench@example.com", "commit", "--quiet", "--allow-empty", "-m", "pinned"},
	} {
		if _, err := gitOutput(ctx, origin, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}

	commit, err := gitOutput(ctx, origin, gitRevParse, gitHead)
	if err != nil {
		t.Fatal(err)
	}

	dir, err := cloneAt(ctx, Entry{Name: "local", Repo: origin, Commit: commit}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if head, _ := gitOutput(ctx, dir, gitRevParse, gitHead); head != commit {
		t.Fatalf("HEAD = %s, want %s", head, commit)
	}

	unknown := Entry{Name: "local", Repo: origin, Commit: strings.Repeat("0", 40)}
	if _, err := cloneAt(ctx, unknown, t.TempDir()); err == nil {
		t.Fatal("a commit the remote does not have: no error")
	}
}

func TestWriteReport(t *testing.T) {
	rep := report{Date: "2026-10-22", Commit: "abc", Results: []result{{Entry: Entry{Name: "x"}}}}

	var printed bytes.Buffer
	if err := writeReport(rep, "", &printed); err != nil || !strings.Contains(printed.String(), "| x |") {
		t.Fatalf("print only: %v, %q", err, printed.String())
	}

	dir := t.TempDir()

	var stdout bytes.Buffer
	if err := writeReport(rep, dir, &stdout); err != nil {
		t.Fatal(err)
	}

	for _, ext := range []string{".json", ".md"} {
		if _, err := os.Stat(filepath.Join(dir, "2026-10-22-abc"+ext)); err != nil {
			t.Errorf("%s not written: %v", ext, err)
		}
	}
}

func (w *cancelOnWrite) Write(p []byte) (int, error) {
	if strings.HasPrefix(string(p), "bench: ") {
		if work, _ := filepath.Glob(filepath.Join(w.dir, workPattern+"*")); len(work) > 0 {
			w.entries++
		}

		w.cancel()
	}

	return len(p), nil
}

// isolateTempDir points os.TempDir at a fresh directory, so a test can check what a run left.
func isolateTempDir(t *testing.T) string {
	t.Helper()

	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	return tmp
}

func assertEmpty(t *testing.T, dir string) {
	t.Helper()

	left, err := filepath.Glob(filepath.Join(dir, workPattern+"*"))
	if err != nil || len(left) != 0 {
		t.Fatalf("the run left %v behind (%v)", left, err)
	}
}
