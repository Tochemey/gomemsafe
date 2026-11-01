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
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// fakeGo is a runner that answers the go commands the conformance run makes, without
// building or fetching anything: the download points at a corpus under fake's directory, the
// builds succeed, and the vet runs print the JSON the test chooses.
type fakeGo struct {
	t          *testing.T
	corpusDir  string
	checklocks string
	gomemsafe  string
	fail       string
	// calls records each command, for the test to check the order.
	calls []string
}

func (f *fakeGo) run(_ context.Context, dir, name string, args ...string) ([]byte, error) {
	f.t.Helper()

	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)

	if f.fail != "" && strings.Contains(call, f.fail) {
		return []byte("boom"), errors.New("exit status 1")
	}

	switch {
	case name != "go":
		f.t.Fatalf("unexpected command %s", call)
	case args[0] == "env":
		return []byte(filepath.Join(f.t.TempDir(), "go.mod") + "\n"), nil
	case args[0] == "mod" && args[1] == "download":
		return []byte(`{"Path": "gvisor.dev/gvisor", "Dir": "` + f.corpusDir + `"}`), nil
	case args[0] == "vet":
		// go vet exits non-zero when the tool reports, with the JSON on its output.
		out := f.checklocks
		if filepath.Base(strings.TrimPrefix(args[1], "-vettool=")) == "gomemsafe" {
			out = f.gomemsafe
		}

		out = strings.ReplaceAll(out, "$DIR", strings.ReplaceAll(dir, `\`, `\\`))
		if strings.Contains(out, "MS") {
			return []byte(out), &exec.ExitError{}
		}

		return []byte(out), nil
	}

	return nil, nil
}

// corpusWith writes a fake gVisor module directory whose checklocks test package has the
// named files, each marked as failing on the given lines, and returns it.
func corpusWith(t *testing.T, marked map[string][]int) string {
	t.Helper()

	root := t.TempDir()
	dir := filepath.Join(root, filepath.FromSlash(testDir))

	for _, f := range corpusFiles {
		var b strings.Builder

		b.WriteString("package test\n")

		for line := 2; line <= 12; line++ {
			if slices.Contains(marked[f.name], line) {
				b.WriteString("var _ = 1 // +checklocksfail\n")
			} else {
				b.WriteString("\n")
			}
		}

		path := filepath.Join(dir, f.name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, "BUILD"), []byte("not go"), 0o600); err != nil {
		t.Fatal(err)
	}

	return root
}

// vetJSON renders one package's diagnostics as go vet -json prints them, with $DIR for the
// corpus directory.
func vetJSON(diags ...string) string {
	return `{"gvisor.dev/gvisor/tools/checklocks/test": {"locks": [` + strings.Join(diags, ",") + `]}}` + "\n"
}

func diag(category, file string, line int, msg string) string {
	return `{"category": "` + category + `", "posn": "$DIR/` + file + `:` + strconv.Itoa(line) + `:1", "message": "` + msg + `"}`
}

func TestConformAgrees(t *testing.T) {
	fake := &fakeGo{
		t:          t,
		corpusDir:  corpusWith(t, map[string][]int{"basics.go": {3, 7}, "closures.go": {4}}),
		checklocks: vetJSON(),
		gomemsafe: vetJSON(diag("MS403", "basics.go", 3, "MS403: a"), diag("MS403", "basics.go", 7, "MS403: b"),
			diag("MS000", "atomics.go", 2, "MS000: +checkatomic is not supported yet")),
	}

	var stdout bytes.Buffer

	out := filepath.Join(t.TempDir(), "conformance.md")

	if err := conform(context.Background(), options{out: out}, &stdout, fake.run); err != nil {
		t.Fatalf("conform: %v\n%s", err, stdout.String())
	}

	table := stdout.String()
	if !strings.Contains(table, "| basics.go | compared | 2 | 2 | - | - | agree |") {
		t.Errorf("table:\n%s", table)
	}

	if !strings.Contains(table, "| closures.go | context: values stored through inlined literals are not followed into fields | 1 | 0 | 4 | - | shown |") {
		t.Errorf("context row is wrong:\n%s", table)
	}

	written, err := os.ReadFile(out) //nolint:gosec // G304: the test's own file
	if err != nil || string(written) != table {
		t.Errorf("-out file: %v, %q", err, written)
	}

	// The corpus module is prepared, both tools are built, checklocks runs first.
	joined := strings.Join(fake.calls, "\n")

	for _, want := range []string{"go env GOMOD", "go mod download -json gvisor.dev/gvisor@" + corpusVersion, "go mod tidy",
		"go get " + checklocksCmd + "@" + toolVersion, "go build -o", "go vet -vettool="} {
		if !strings.Contains(joined, want) {
			t.Errorf("no call %q in:\n%s", want, joined)
		}
	}

	if vets := strings.Count(joined, "go vet"); vets != 2 || strings.Index(joined, "checklocks -json") > strings.Index(joined, "gomemsafe -json") {
		t.Errorf("vet runs are wrong:\n%s", joined)
	}
}

func TestConformFailsOnDisagreement(t *testing.T) {
	fake := &fakeGo{
		t:          t,
		corpusDir:  corpusWith(t, map[string][]int{"basics.go": {3}}),
		checklocks: vetJSON(),
		gomemsafe:  vetJSON(diag("MS403", "basics.go", 5, "MS403: a")),
	}

	var stdout bytes.Buffer

	err := conform(context.Background(), options{}, &stdout, fake.run)
	if err == nil || !strings.Contains(err.Error(), "basics.go") {
		t.Fatalf("conform: %v", err)
	}

	if !strings.Contains(stdout.String(), "| basics.go | compared | 1 | 1 | 3 | 5 | DIFFER |") {
		t.Errorf("table:\n%s", stdout.String())
	}
}

func TestConformFailsWhenChecklocksReports(t *testing.T) {
	fake := &fakeGo{
		t:          t,
		corpusDir:  corpusWith(t, nil),
		checklocks: vetJSON(diag("MS403", "basics.go", 2, "missing expected failure")),
		gomemsafe:  vetJSON(),
	}

	err := conform(context.Background(), options{}, &bytes.Buffer{}, fake.run)
	if err == nil || !strings.Contains(err.Error(), "does not pass its own tests") || !strings.Contains(err.Error(), "basics.go:2:1") {
		t.Fatalf("conform: %v", err)
	}
}

// TestConformFailsWhenACommandFails fails each go command in turn.
func TestConformFailsWhenACommandFails(t *testing.T) {
	for _, step := range []string{"env", "mod download", "mod tidy", "go get", "build -o", "vet"} {
		fake := &fakeGo{t: t, corpusDir: corpusWith(t, nil), checklocks: vetJSON(), gomemsafe: vetJSON(), fail: step}

		err := conform(context.Background(), options{}, &bytes.Buffer{}, fake.run)
		if err == nil || !strings.Contains(err.Error(), "boom") && step != "env" {
			t.Errorf("%s failing: conform = %v", step, err)
		}
	}
}

func TestConformFailsOnBadDownloadOutputAndMissingFiles(t *testing.T) {
	fake := &fakeGo{t: t, corpusDir: "", checklocks: vetJSON(), gomemsafe: vetJSON()}

	if err := conform(context.Background(), options{}, &bytes.Buffer{}, fake.run); err == nil {
		t.Error("an empty download directory did not fail")
	}

	fake.corpusDir = t.TempDir()

	if err := conform(context.Background(), options{}, &bytes.Buffer{}, fake.run); err == nil {
		t.Error("a corpus without the test files did not fail")
	}

	fake.corpusDir = corpusWith(t, nil)
	fake.gomemsafe = `{"pkg": {"locks": {"error": "analyzer failed"}}}`

	if err := conform(context.Background(), options{}, &bytes.Buffer{}, fake.run); err == nil || !strings.Contains(err.Error(), "analyzer failed") {
		t.Errorf("an analyzer failure: conform = %v", err)
	}

	fake.gomemsafe = `{"pkg": {"locks": [`

	if err := conform(context.Background(), options{}, &bytes.Buffer{}, fake.run); err == nil {
		t.Error("truncated JSON did not fail")
	}

	fake.gomemsafe = vetJSON()

	if err := conform(context.Background(), options{out: filepath.Join(t.TempDir(), "missing", "x.md")}, &bytes.Buffer{}, fake.run); err == nil {
		t.Error("an unwritable -out did not fail")
	}
}

func TestModuleRootNeedsAModule(t *testing.T) {
	run := func(context.Context, string, string, ...string) ([]byte, error) {
		return []byte(os.DevNull + "\n"), nil
	}
	if _, err := moduleRoot(context.Background(), run); err == nil {
		t.Fatal("a run outside a module did not fail")
	}
}

func TestRunConformanceExitCodes(t *testing.T) {
	var stderr bytes.Buffer

	if code := runConformance([]string{"-bogus"}, &bytes.Buffer{}, &stderr, nil); code != exitUsage {
		t.Errorf("bad flag: exit %d", code)
	}

	if code := runConformance([]string{"extra"}, &bytes.Buffer{}, &stderr, nil); code != exitUsage {
		t.Errorf("an argument: exit %d", code)
	}

	fake := &fakeGo{t: t, corpusDir: corpusWith(t, nil), checklocks: vetJSON(), gomemsafe: vetJSON(), fail: "mod tidy"}
	if code := runConformance(nil, &bytes.Buffer{}, &stderr, fake.run); code != exitError || !strings.Contains(stderr.String(), "conformance:") {
		t.Errorf("a failing run: exit %d, stderr %q", code, stderr.String())
	}

	fake.fail = ""
	if code := runConformance(nil, &bytes.Buffer{}, &stderr, fake.run); code != exitOK {
		t.Errorf("a clean run: exit %d", code)
	}
}

func TestCommandOutputRunsGo(t *testing.T) {
	out, err := commandOutput(context.Background(), "", "go", "version")
	if err != nil || !strings.HasPrefix(string(out), "go version") {
		t.Fatalf("go version: %v, %q", err, out)
	}

	if _, err := commandOutput(context.Background(), "", "go", "nonsense-subcommand"); err == nil {
		t.Fatal("a failing command did not fail")
	}
}
