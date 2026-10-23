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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tochemey/gomemsafe/internal/trace"
)

// fakeGoPrelude starts the fake go command: n is the number of this call, counted in a file
// next to the script.
const fakeGoPrelude = `#!/bin/sh
n=$(( $(cat "$0.calls" 2>/dev/null || echo 0) + 1 ))
echo "$n" > "$0.calls"
`

func TestMostImportedPicksTheMostImportedPackage(t *testing.T) {
	listing := "a\t/m/a\ta2.go,a1.go\tb,c\n" +
		"b\t/m/b\tb.go\tc\n" +
		"c\t/m/c\tc.go\tfmt\n" +
		"d\t/m/d\t\tc\n" + // no Go files: never a target
		"malformed line\n"

	got, err := mostImported([]byte(listing))
	if err != nil {
		t.Fatal(err)
	}

	if got.importPath != "c" || got.file != filepath.Join("/m/c", "c.go") {
		t.Fatalf("target = %+v, want c/c.go", got)
	}

	if _, err := mostImported(nil); err == nil {
		t.Fatal("empty listing: no error")
	}
}

func TestCountLinesSkipsTests(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.go"), "package a\n\nfunc A() {}\n")
	write(t, filepath.Join(dir, "a_test.go"), "package a\n\n\n\n\n")

	if n, err := countLines(dir); err != nil || n != 3 {
		t.Fatalf("countLines = %d, %v; want 3", n, err)
	}
}

func TestReadTraceAggregates(t *testing.T) {
	dir := t.TempDir()

	var lines []byte

	for _, e := range []trace.Event{
		{Package: "p", Kind: trace.KindRun, Nanos: 3e6, MaxRSS: 10, Elapsed: 5e6},
		{Package: "p", Kind: trace.KindSSA, Nanos: 1e6, MaxRSS: 30, Elapsed: 4e6},
		{Package: "q", Kind: trace.KindBudget, MaxRSS: 20, Elapsed: 2e6},
	} {
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}

		lines = append(append(lines, b...), '\n')
	}

	write(t, filepath.Join(dir, "1"+traceExt), string(lines))

	sum, err := readTrace(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(sum.packages) != 2 || sum.analyzerTime != 3*time.Millisecond || sum.ssaTime != time.Millisecond ||
		sum.budgetHits != 1 || sum.rssByPackage["p"] != 30 || sum.processTime["p"] != 5*time.Millisecond {
		t.Fatalf("summary = %+v", sum)
	}

	write(t, filepath.Join(dir, "2"+traceExt), "{broken\n")

	if _, err := readTrace(dir); err == nil {
		t.Fatal("malformed trace: no error")
	}

	if sum, err := readTrace(filepath.Join(dir, "missing")); err != nil || len(sum.packages) != 0 {
		t.Fatalf("missing directory: %+v, %v; want an empty summary", sum, err)
	}
}

func TestReadRSSAndMaxRatio(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rss.tsv")
	write(t, path, "p\t100\np\t150\nq\t50\n\n")

	compile, err := readRSS(path)
	if err != nil || compile["p"] != 150 || compile["q"] != 50 {
		t.Fatalf("readRSS = %v, %v", compile, err)
	}

	ratio, pkg := maxRatio(map[string]int64{"p": 300, "q": 50, "r": 999}, compile)
	if ratio != 2 || pkg != "p" {
		t.Fatalf("maxRatio = %v, %q; want 2 for p", ratio, pkg)
	}

	if maxValue(compile) != 150 {
		t.Fatalf("maxValue = %d", maxValue(compile))
	}

	write(t, path, "p\tlots\n")

	if _, err := readRSS(path); err == nil {
		t.Fatal("malformed RSS line: no error")
	}
}

func TestMeasureRunsTheRSSScenarioAlone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("peak RSS is recorded on unix only")
	}

	ctx := context.Background()

	root, err := gitOutput(ctx, "", gitRevParse, gitTopLevel)
	if err != nil {
		t.Fatal(err)
	}

	work := t.TempDir()

	tl, err := buildTools(ctx, root, work)
	if err != nil {
		t.Fatal(err)
	}

	dir, err := generateSynthetic(filepath.Join(work, syntheticEntry), true)
	if err != nil {
		t.Fatal(err)
	}

	e := Entry{Name: syntheticEntry, Synthetic: true, Small: true, Patterns: []string{"./..."}}

	m, err := measure(ctx, dir, e, tl, map[string]bool{scenarioRSS: true}, filepath.Join(work, "measure"))
	if err != nil {
		t.Fatal(err)
	}

	if m.MaxToolRSS == 0 || m.MaxCompileRSS == 0 {
		t.Fatalf("gomemsafe RSS = %d, compile RSS = %d; want both measured", m.MaxToolRSS, m.MaxCompileRSS)
	}
}

// TestMeasureStopsAtTheFirstFailure runs measure with a fake go command. With every scenario,
// the calls are: 1 cold vet, 2 cold gomemsafe, then 3 the compile for rss; 3 and 4 the warm
// runs; or 3 go list and 4 gomemsafe after the edit.
func TestMeasureStopsAtTheFirstFailure(t *testing.T) {
	pkg := t.TempDir()
	write(t, filepath.Join(pkg, "p.go"), "package p\n")

	readOnly := t.TempDir()
	write(t, filepath.Join(readOnly, "p.go"), "package p\n")

	if err := os.Chmod(filepath.Join(readOnly, "p.go"), 0o400); err != nil {
		t.Fatal(err)
	}

	list := func(dir string) string {
		return fmt.Sprintf(`[ "$1" = list ] && printf 'example.com/p\t%s\tp.go\t\n'`, dir)
	}

	cold := map[string]bool{scenarioCold: true}
	rss := map[string]bool{scenarioRSS: true}
	warm := map[string]bool{scenarioWarm: true}
	edit := map[string]bool{scenarioEdit: true}

	tests := []struct {
		name      string
		scenarios map[string]bool
		script    string
		want      string
	}{
		{"cold vet", cold, `[ "$n" = 1 ] && exit 2`, "exit status 2"},
		{"cold gomemsafe", cold, `[ "$n" = 2 ] && exit 2`, "exit status 2"},
		{"cold output", cold, `[ "$n" = 2 ] && echo 'not json'`, "invalid character"},
		{"cold trace", cold, `[ "$n" = 2 ] && echo 'not json' > "$GOMEMSAFE_TRACE/bad.jsonl"`, "bad.jsonl"},
		{"compile", rss, `[ "$n" = 3 ] && exit 2`, "exit status 2"},
		{"compile RSS", rss, ``, compileRSSFile},
		{"warm vet", warm, `[ "$n" = 3 ] && exit 2`, "exit status 2"},
		{"warm gomemsafe", warm, `[ "$n" = 4 ] && exit 2`, "exit status 2"},
		{"go list", edit, `[ "$n" = 3 ] && exit 2`, "exit status 2"},
		{"nothing to edit", edit, ``, "no package with Go files"},
		{"missing package", edit, list(filepath.Join(pkg, "missing")), "missing"},
		{"missing file", edit, list(t.TempDir()), "p.go: no such file"},
		{"read-only file", edit, list(readOnly), "p.go: permission denied"},
		{"edited gomemsafe", edit, list(pkg) + "\n" + `[ "$n" = 4 ] && exit 2`, "exit status 2"},
		{"edited trace", edit, list(pkg) + "\n" + `[ "$n" = 4 ] && echo 'not json' > "$GOMEMSAFE_TRACE/bad.jsonl"`, "bad.jsonl"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeGo(t, tt.script)

			e := Entry{Name: "p", Patterns: []string{"./..."}}

			_, err := measure(context.Background(), t.TempDir(), e, tools{}, tt.scenarios, t.TempDir())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want one mentioning %q", err, tt.want)
			}
		})
	}
}

func TestCountLinesFailures(t *testing.T) {
	if _, err := countLines("["); err == nil {
		t.Error("a directory that is not a valid pattern: no error")
	}

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "x.go"), 0o750); err != nil {
		t.Fatal(err)
	}

	if _, err := countLines(dir); err == nil {
		t.Error("a directory named like a Go file: no error")
	}
}

func TestReadTraceFailures(t *testing.T) {
	if _, err := readTrace("["); err == nil {
		t.Error("a directory that is not a valid pattern: no error")
	}

	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "x"+traceExt)); err != nil {
		t.Fatal(err)
	}

	if _, err := readTrace(dir); err == nil {
		t.Error("a trace file that cannot be opened: no error")
	}
}

func TestTimeVetToleratesFindingsOnly(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module example.com/x\n\ngo 1.26\n")
	write(t, filepath.Join(dir, "x.go"), "package x\n\nimport \"fmt\"\n\nfunc F() { fmt.Printf(\"%d\") }\n")

	if _, err := timeVet(context.Background(), dir, []string{"vet", "./..."}, ""); err != nil {
		t.Fatalf("go vet with a finding: %v, want a timing", err)
	}

	if _, err := timeVet(context.Background(), dir, []string{"vet", "-notaflag", "./..."}, ""); err == nil {
		t.Fatal("go vet with a usage error: no error")
	}
}

func TestTimeGoFailsOnAnyNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "go.mod"), "module example.com/x\n\ngo 1.26\n")
	write(t, filepath.Join(dir, "x.go"), "package x\n\nfunc F() { undefined() }\n")

	if _, _, err := timeGo(context.Background(), dir, []string{"vet", "./..."}, "", filepath.Join(t.TempDir(), "trace")); err == nil {
		t.Fatal("go vet on a package that does not build: no error")
	}

	if _, _, err := timeGo(context.Background(), filepath.Join(dir, "missing"), []string{"version"}, "", ""); err == nil {
		t.Fatal("a command that cannot start: no error")
	}

	if _, _, err := timeGo(context.Background(), dir, []string{"version"}, "", ""); err != nil {
		t.Fatalf("go version: %v", err)
	}

	file := filepath.Join(t.TempDir(), "file")
	write(t, file, "")

	if _, _, err := timeGo(context.Background(), dir, []string{"version"}, "", filepath.Join(file, "trace")); err == nil {
		t.Fatal("a trace directory under a file: no error")
	}
}

func TestFindingsAreRelativeToTheCorpus(t *testing.T) {
	dir := t.TempDir()
	output := `{"p": {"lifetime": [{"category": "MS301", "posn": "` + filepath.Join(dir, "a.go") + `:3:4", "message": "m"},
		{"category": "MS301", "posn": "/elsewhere/b.go:1:1", "message": "n"}]}}`

	got, err := findings(dir, []byte(output))
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].Posn != "/elsewhere/b.go:1:1" || got[1].Posn != "a.go:3:4" || got[1].Category != "MS301" {
		t.Fatalf("findings = %+v", got)
	}

	if _, err := findings(dir, []byte("{")); err == nil {
		t.Fatal("malformed output: no error")
	}
}

func TestLastLinesKeepsTheEnd(t *testing.T) {
	if got := lastLines("a\nb\nc\n", 2); got != "b\nc" {
		t.Fatalf("lastLines = %q", got)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// fakeGo puts a go command first on PATH that runs fakeGoPrelude, then script, and otherwise
// succeeds without output.
func fakeGo(t *testing.T, script string) {
	t.Helper()

	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(fakeGoPrelude+script+"\nexit 0\n"), 0o700); err != nil { //nolint:gosec // G306: the fake go command must be executable
		t.Fatal(err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
