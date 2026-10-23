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
	"errors"
	"flag"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// mainEnv makes a child process of the test binary run main instead of the tests.
	mainEnv = "GOMEMSAFE_TEST_MAIN"
	// pinnedTag tags the commit of pinnedOrigin.
	pinnedTag = "v1"
)

// cancelOnWrite is a stderr that cancels the run when it announces an entry, counting the
// announcements made while the run's work directory exists in dir.
type cancelOnWrite struct {
	cancel  context.CancelFunc
	dir     string
	entries int
}

func TestMain(m *testing.M) {
	if os.Getenv(mainEnv) != "" {
		main()
	}

	os.Exit(m.Run())
}

func TestMainPrintsUsage(t *testing.T) {
	out, err := mainCommand(t, "-h").CombinedOutput()

	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(string(out), "Usage of bench") {
		t.Fatalf("err = %v, output %q; want exit 2 and the usage", err, out)
	}
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

	stderr.Reset()

	if code := runBench([]string{"-scenario", "nope"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "bench: unknown scenario") {
		t.Fatalf("unknown scenario: exit %d, stderr %q; want 2 and the error", code, stderr.String())
	}
}

func TestBenchmarkFailsOutsideARepository(t *testing.T) {
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer

	if code := runBench(nil, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), gitTopLevel) {
		t.Fatalf("exit %d, stderr %q; want 1 and the git error", code, stderr.String())
	}
}

func TestBenchmarkFailsWithoutATemporaryDirectory(t *testing.T) {
	corpus := syntheticCorpus(t, "./...")
	missing := filepath.Join(t.TempDir(), "missing")
	t.Setenv("TMPDIR", missing)

	var stdout, stderr bytes.Buffer

	if code := runBench([]string{"-corpus-file", corpus}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), missing) {
		t.Fatalf("exit %d, stderr %q; want 1 and the temporary directory error", code, stderr.String())
	}
}

func TestBenchmarkFailsWhenTheToolsDoNotBuild(t *testing.T) {
	corpus := syntheticCorpus(t, "./...")
	tmp := isolateTempDir(t)
	repo := emptyRepository(t)
	t.Chdir(repo)

	var stdout, stderr bytes.Buffer

	if code := runBench([]string{"-corpus-file", corpus}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), gomemsafePackage) {
		t.Fatalf("exit %d, stderr %q; want 1 and the build error", code, stderr.String())
	}

	assertEmpty(t, tmp)
}

func TestBenchmarkFailsWhenAnEntryFails(t *testing.T) {
	tests := []struct{ name, corpus, want string }{
		{"clone", `[{"name": "remote", "repo": "` + filepath.Join(t.TempDir(), "missing") + `", "commit": "abc", "patterns": ["./..."]}]`, "remote: "},
		{"measure", `[{"name": "synthetic", "synthetic": true, "small": true, "patterns": ["./missing"]}]`, "synthetic: "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			corpus := filepath.Join(t.TempDir(), corpusFileName)
			write(t, corpus, tt.corpus)

			tmp := isolateTempDir(t)

			var stdout, stderr bytes.Buffer

			if code := runBench([]string{"-corpus-file", corpus}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "bench: "+tt.want) {
				t.Fatalf("exit %d, stderr %q; want 1 and the entry's error", code, stderr.String())
			}

			assertEmpty(t, tmp)
		})
	}
}

func TestNewReportOfARepositoryWithoutCommits(t *testing.T) {
	if got := newReport(context.Background(), emptyRepository(t)).Commit; got != "unknown" {
		t.Fatalf("commit = %q, want unknown", got)
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

func TestPrepareSourceClonesTheEntry(t *testing.T) {
	origin, commit := pinnedOrigin(t)
	work := t.TempDir()
	e := Entry{Name: "local", Repo: origin, Commit: commit, Dir: "sub"}

	dir, err := prepareSource(context.Background(), e, work)
	if err != nil {
		t.Fatal(err)
	}

	if want := filepath.Join(work, clonesDir, e.cloneName(), "sub"); dir != want {
		t.Fatalf("dir = %s, want %s", dir, want)
	}
}

func TestCloneAtFailsWhenHEADCannotBeRead(t *testing.T) {
	origin, commit := pinnedOrigin(t)

	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}

	// A git that fails only rev-parse, which a real one does not after a checkout.
	dir := t.TempDir()
	script := "#!/bin/sh\n[ \"$1\" = " + gitRevParse + " ] && exit 2\nexec '" + git + "' \"$@\"\n"

	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o700); err != nil { //nolint:gosec // G306: the fake git must be executable
		t.Fatal(err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if _, err := cloneAt(context.Background(), Entry{Name: "local", Repo: origin, Commit: commit}, t.TempDir()); err == nil ||
		!strings.Contains(err.Error(), gitRevParse) {
		t.Fatalf("err = %v, want the rev-parse error", err)
	}
}

func TestCloneAtPinsTheCommit(t *testing.T) {
	ctx := context.Background()
	origin, commit := pinnedOrigin(t)

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

	// A tag names the commit, but it is not the commit the entry must pin.
	tagged := Entry{Name: "local", Repo: origin, Commit: pinnedTag}
	if _, err := cloneAt(ctx, tagged, t.TempDir()); err == nil || !strings.Contains(err.Error(), "want the pinned "+pinnedTag) {
		t.Fatalf("a tag instead of a commit: %v, want the pin error", err)
	}

	file := filepath.Join(t.TempDir(), "file")
	write(t, file, "")

	if _, err := cloneAt(ctx, Entry{Name: "local", Repo: origin, Commit: commit}, file); err == nil {
		t.Fatal("a parent that is a file: no error")
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

func TestWriteReportFailures(t *testing.T) {
	rep := report{Date: "2026-10-22", Commit: "abc"}
	file := filepath.Join(t.TempDir(), "file")
	write(t, file, "")

	if err := writeReport(rep, filepath.Join(file, "out"), new(bytes.Buffer)); err == nil {
		t.Error("an output directory under a file: no error")
	}

	nan := report{Results: []result{{Measurement: measurement{MaxRSSRatio: math.NaN()}}}}
	if err := writeReport(nan, t.TempDir(), new(bytes.Buffer)); err == nil {
		t.Error("a ratio JSON cannot encode: no error")
	}

	for _, ext := range []string{".json", ".md"} {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "2026-10-22-abc"+ext), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := writeReport(rep, dir, new(bytes.Buffer)); err == nil {
			t.Errorf("a directory where the %s file goes: no error", ext)
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

// syntheticCorpus writes a corpus file with the small generated corpus, analysed with pattern.
func syntheticCorpus(t *testing.T, pattern string) string {
	t.Helper()

	corpus := filepath.Join(t.TempDir(), corpusFileName)
	write(t, corpus, `[{"name": "synthetic", "synthetic": true, "small": true, "patterns": ["`+pattern+`"]}]`)

	return corpus
}

// emptyRepository returns a new git repository without commits.
func emptyRepository(t *testing.T) string {
	t.Helper()

	repo := t.TempDir()
	if _, err := gitOutput(context.Background(), repo, "init", gitQuiet); err != nil {
		t.Fatal(err)
	}

	return repo
}

// pinnedOrigin returns a repository with one commit, tagged pinnedTag, and that commit.
func pinnedOrigin(t *testing.T) (string, string) {
	t.Helper()

	ctx := context.Background()
	origin := emptyRepository(t)

	for _, args := range [][]string{
		{"-c", "user.name=bench", "-c", "user.email=bench@example.com", "commit", "--quiet", "--allow-empty", "-m", "pinned"},
		{"tag", pinnedTag},
	} {
		if _, err := gitOutput(ctx, origin, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}

	commit, err := gitOutput(ctx, origin, gitRevParse, gitHead)
	if err != nil {
		t.Fatal(err)
	}

	return origin, commit
}

// mainCommand runs main with args in a child process of the test binary. The child writes its
// coverage where the test binary does, so the coverage profile includes main.
func mainCommand(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(self, args...) //nolint:gosec // G204: the test binary, with test-chosen arguments

	cmd.Env = append(os.Environ(), mainEnv+"=1")

	if testing.CoverMode() != "" {
		dir := flag.Lookup("test.gocoverdir").Value.String()
		if dir == "" {
			dir = t.TempDir()
		}

		cmd.Env = append(cmd.Env, "GOCOVERDIR="+dir)
	}

	return cmd
}
