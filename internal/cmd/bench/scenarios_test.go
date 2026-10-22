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
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/tochemey/gomemsafe/internal/trace"
)

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
