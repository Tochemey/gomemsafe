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
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/txtar"

	"github.com/tochemey/gomemsafe/internal/trace"
)

// binary is gomemsafe, built once for the integration tests, which run the real go vet.
var binary string

// failingWriter fails every write.
type failingWriter struct{}

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func TestMainReportsFindingsAndExitsOne(t *testing.T) {
	code, out, _ := runMain(t, fixture(t, "findings"))
	if code != ExitFindings {
		t.Fatalf("exit code = %d, want %d", code, ExitFindings)
	}

	for _, want := range []string{
		"findings.go:13:2: MS301: buf used after it was returned to sync.Pool\n" +
			"    buf.WriteString(p)\n" +
			"    ^^^\n",
		"findings.go:12:10: buf returned to sync.Pool here\n    pool.Put(buf)\n",
		"  help: move pool.Put(buf) after the last use, or use `defer pool.Put(buf)`\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestMainCleanModuleExitsZero(t *testing.T) {
	code, out, errOut := runMain(t, fixture(t, "clean"))
	if code != ExitClean || out != "" {
		t.Fatalf("exit code = %d, output %q, stderr %q; want 0 and no output", code, out, errOut)
	}
}

func TestMainBuildFailureExitsTwo(t *testing.T) {
	code, _, errOut := runMain(t, fixture(t, "broken"))
	if code != ExitError || !strings.Contains(errOut, "go vet failed") {
		t.Fatalf("exit code = %d, stderr %q; want 2 and the go vet failure", code, errOut)
	}
}

func TestMainJSONPassesVetOutputThrough(t *testing.T) {
	code, out, _ := runMain(t, fixture(t, "findings"), "-json")
	if code != ExitFindings {
		t.Fatalf("exit code = %d, want %d", code, ExitFindings)
	}

	res, err := decode([]byte(out))
	if err != nil {
		t.Fatalf("output is not vet JSON: %v\n%s", err, out)
	}

	if len(res.diagnostics) != 1 || res.diagnostics[0].Category != "MS301" {
		t.Fatalf("diagnostics = %+v, want one MS301", res.diagnostics)
	}
}

func TestMainForwardsAnalyzerFlags(t *testing.T) {
	if code, out, _ := runMain(t, fixture(t, "findings"), "-lifetime=false"); code != ExitClean {
		t.Fatalf("with -lifetime=false: exit code = %d, output:\n%s", code, out)
	}
}

func TestMainHelpPrintsUsage(t *testing.T) {
	code, _, errOut := runMain(t, t.TempDir(), "-h")
	if code != ExitError || !strings.Contains(errOut, "usage: gomemsafe") || strings.Contains(errOut, "help requested") {
		t.Fatalf("exit code = %d, stderr %q", code, errOut)
	}
}

func TestMainFailsWhenOutputCannotBeWritten(t *testing.T) {
	t.Chdir(fixture(t, "findings"))

	if code := Main(nil, failingWriter{}, failingWriter{}); code != ExitError {
		t.Fatalf("exit code = %d, want %d when stdout fails", code, ExitError)
	}
}

func TestMainCachesRunsWithFindings(t *testing.T) {
	dir := fixture(t, "findings")

	first := t.TempDir()
	t.Setenv(trace.EnvVar, first)

	code1, out1, _ := runMain(t, dir)

	second := t.TempDir()
	t.Setenv(trace.EnvVar, second)

	code2, out2, _ := runMain(t, dir)

	if code1 != ExitFindings || code2 != ExitFindings || out1 != out2 {
		t.Fatalf("runs differ: exit %d then %d, output %q then %q", code1, code2, out1, out2)
	}

	if n := countEvents(t, first, "example.com/findings"); n == 0 {
		t.Fatal("first run recorded no events for the package")
	}

	if n := countEvents(t, second, "example.com/findings"); n != 0 {
		t.Fatalf("second run recorded %d events for the package, want a cache hit", n)
	}
}

func TestMainSkipsChecksInFactsOnlyRuns(t *testing.T) {
	t.Setenv("GOCACHE", t.TempDir())

	traceDir := t.TempDir()
	t.Setenv(trace.EnvVar, traceDir)

	runMain(t, fixture(t, "findings"))

	var skippedDeps, skippedRoots int

	for _, e := range readEvents(t, traceDir) {
		if e.Kind != trace.KindSkipped {
			continue
		}

		if e.VetxOnly {
			skippedDeps++
		} else {
			skippedRoots++
		}
	}

	// If unitchecker stops running checks in facts-only runs, nothing is skipped any more and
	// this test fails, which is the signal to drop the short-circuit.
	if skippedDeps == 0 {
		t.Fatal("no check was skipped in a facts-only run")
	}

	if skippedRoots != 0 {
		t.Fatalf("%d checks were skipped on root packages, want 0", skippedRoots)
	}
}

func TestFactsAreByteIdenticalAcrossBodyOnlyEdit(t *testing.T) {
	dir := fixture(t, "crosspkg")
	before := vetxOf(t, dir, "example.com/cross/a")

	pkgA, err := os.OpenRoot(filepath.Join(dir, "a"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pkgA.Close() }()

	src, err := pkgA.ReadFile("a.go")
	if err != nil {
		t.Fatal(err)
	}

	// Work's body changes but no summary does. The go command still re-runs a's dependents
	// when a's compiled export data changes; what gomemsafe controls is that a's facts file
	// comes out byte-identical, so the facts never cause a re-run themselves.
	edited := strings.Replace(string(src), "s += i", "s -= i", 1)
	if err := pkgA.WriteFile("a.go", []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}

	if after := vetxOf(t, dir, "example.com/cross/a"); !bytes.Equal(before, after) {
		t.Fatalf("a's facts file changed across a body-only edit (%d bytes, then %d)", len(before), len(after))
	}

	if code, out, _ := runMain(t, dir); code != ExitFindings ||
		!strings.Contains(out, "buf used after the call to a.Release invalidated it") {
		t.Fatalf("exit code %d, output:\n%s", code, out)
	}
}

func TestMainPrintsStats(t *testing.T) {
	code, _, errOut := runMain(t, fixture(t, "clean"), "-stats")
	if code != ExitClean {
		t.Fatalf("exit code = %d, stderr:\n%s", code, errOut)
	}

	for _, want := range []string{"gomemsafe stats:", "dataflow budget hits: 0", "lifetime"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stats lack %q:\n%s", want, errOut)
		}
	}
}

// TestMainStatsKeepsFlagValues passes a vet flag followed by its value: -stats must not split
// them.
func TestMainStatsKeepsFlagValues(t *testing.T) {
	code, _, errOut := runMain(t, fixture(t, "clean"), "-stats", "-tags", "integration", "./...")
	if code != ExitClean {
		t.Fatalf("exit code = %d, stderr:\n%s", code, errOut)
	}
}

func TestMainPassesGoWarningsThrough(t *testing.T) {
	dir := fixture(t, "clean")
	if err := os.Mkdir(filepath.Join(dir, "empty"), 0o750); err != nil {
		t.Fatal(err)
	}

	code, _, errOut := runMain(t, dir, "./...", "./empty/...")
	if code != ExitClean || !strings.Contains(errOut, "matched no packages") {
		t.Fatalf("exit code = %d, stderr %q; want 0 and the go command's warning", code, errOut)
	}
}

func TestParseArgs(t *testing.T) {
	o, err := parseArgs([]string{"-json", "-stats", "-lifetime=false", "./a", "-notaflag"})
	if err != nil {
		t.Fatal(err)
	}

	if !o.json || !o.stats || len(o.vetFlags) != 1 || o.vetFlags[0] != "-lifetime=false" {
		t.Fatalf("options = %+v", o)
	}

	if len(o.patterns) != 2 || o.patterns[0] != "./a" || o.patterns[1] != "-notaflag" {
		t.Fatalf("patterns = %q, want everything from the first non-flag on", o.patterns)
	}

	if o, _ := parseArgs(nil); len(o.patterns) != 1 || o.patterns[0] != "./..." {
		t.Fatalf("default patterns = %q, want ./...", o.patterns)
	}

	if _, err := parseArgs([]string{"--help"}); !errors.Is(err, errHelp) {
		t.Fatalf("--help: err = %v, want errHelp", err)
	}
}

func TestDecodeMergesPackagesAndDropsDuplicates(t *testing.T) {
	stream := `{"p": {"lifetime": [{"category": "MS301", "posn": "/x/b.go:2:1", "message": "m"}]}}
{"p [p.test]": {"lifetime": [{"category": "MS301", "posn": "/x/b.go:2:1", "message": "m"},
	{"category": "MS301", "posn": "/x/a.go:9:1", "message": "n"}]}}
{"q": {"lifetime": {"error": "boom"}}}`

	res, err := decode([]byte(stream))
	if err != nil {
		t.Fatal(err)
	}

	if len(res.diagnostics) != 2 || res.diagnostics[0].Posn != "/x/a.go:9:1" {
		t.Fatalf("diagnostics = %+v, want two, sorted by position", res.diagnostics)
	}

	if len(res.errors) != 1 || !strings.Contains(res.errors[0], "q: lifetime: boom") {
		t.Fatalf("errors = %q", res.errors)
	}

	if exitCode(res) != ExitError {
		t.Fatalf("exitCode = %d, want %d when an analyzer failed", exitCode(res), ExitError)
	}
}

func TestDecodeIsTheExportedForm(t *testing.T) {
	diags, errs, err := Decode([]byte(`{"p": {"lifetime": [{"category": "MS301", "posn": "/x/a.go:1:1", "message": "m"}]}}`))
	if err != nil || len(diags) != 1 || diags[0].Category != "MS301" || len(errs) != 0 {
		t.Fatalf("Decode = %+v, %q, %v", diags, errs, err)
	}
}

// FuzzDecode feeds arbitrary bytes to the vet JSON decoder. It must never panic, and decoding
// the same bytes twice gives the same diagnostics and errors, whatever the map order.
func FuzzDecode(f *testing.F) {
	for _, seed := range []string{
		`{"p": {"lifetime": [{"category": "MS301", "posn": "a.go:1:2", "message": "m"}]}}`,
		`{"p": {"lifetime": {"error": "boom"}}}`,
		`{"p": {"lifetime": []}}{"p [p.test]": {"lifetime": [{"posn": "a.go:1:2", "message": "m"}]}}`,
		`{"p": {"lifetime": [{"related": [{"posn": "b.go:3:4", "message": "r"}], "suggested_fixes": [{"message": "h"}]}]}}`,
		`{}`, ``, `{`, `[]`, `{"p": 1}`,
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		first, firstErrs, err1 := Decode(data)
		second, secondErrs, err2 := Decode(data)

		if (err1 == nil) != (err2 == nil) || len(first) != len(second) || len(firstErrs) != len(secondErrs) {
			t.Fatalf("decoding %q twice: %d/%d diagnostics, %d/%d errors, %v/%v",
				data, len(first), len(second), len(firstErrs), len(secondErrs), err1, err2)
		}
	})
}

func TestDecodeRejectsMalformedOutput(t *testing.T) {
	for _, stream := range []string{`{"p": `, `{"p": {"lifetime": 3}}`} {
		if _, err := decode([]byte(stream)); err == nil {
			t.Errorf("decode(%q): no error", stream)
		}
	}
}

func TestExitCode(t *testing.T) {
	if got := exitCode(results{}); got != ExitClean {
		t.Fatalf("no findings: %d", got)
	}

	if got := exitCode(results{diagnostics: []Diagnostic{{}}}); got != ExitFindings {
		t.Fatalf("findings: %d", got)
	}
}

func TestPrinterKeepsFirstError(t *testing.T) {
	p := &printer{w: failingWriter{}}
	p.printf("a")
	p.write([]byte("b"))

	if p.err == nil {
		t.Fatal("printer lost the write error")
	}
}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

// command runs the go command with arguments the test chose.
func command(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...) //nolint:gosec // G204: test-controlled binary and arguments
}

// runTests builds gomemsafe, isolates the go command from the developer's environment, and
// runs the tests.
func runTests(m *testing.M) int {
	dir, err := os.MkdirTemp("", "gomemsafe-frontend-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()

	binary = filepath.Join(dir, "gomemsafe")

	build := command("go", "build", "-o", binary, "github.com/tochemey/gomemsafe/cmd/gomemsafe")
	build.Stdout, build.Stderr = os.Stderr, os.Stderr

	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "building gomemsafe:", err)
		return 1
	}

	for k, v := range map[string]string{
		"GOCACHE": filepath.Join(dir, "gocache"), "GOFLAGS": "", "GOWORK": "off",
		"GOTOOLCHAIN": "local", "GOPROXY": "off", trace.EnvVar: "",
	} {
		if err := os.Setenv(k, v); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}

	executable = func() (string, error) { return binary, nil }

	return m.Run()
}

// fixture writes testdata/<name>.txtar into a fresh directory and returns it.
func fixture(t *testing.T, name string) string {
	t.Helper()

	archive, err := txtar.ParseFile(filepath.Join("testdata", name+".txtar"))
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()

	for _, f := range archive.Files {
		path := filepath.Join(dir, f.Name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, f.Data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

// runMain runs the front-end in dir and returns its exit code, stdout, and stderr.
func runMain(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	t.Chdir(dir)

	var out, errOut bytes.Buffer

	code := Main(args, &out, &errOut)

	return code, out.String(), errOut.String()
}

// vetxOf runs go vet in dir, keeping its work directory, and returns the facts file that
// gomemsafe wrote for the package importPath. The package must miss the cache, or no file is
// written.
func vetxOf(t *testing.T, dir, importPath string) []byte {
	t.Helper()

	cmd := command("go", "vet", "-vettool="+binary, "-json", "-work", "-x", "./...")
	cmd.Dir = dir

	out, _ := cmd.CombinedOutput() // the fixture has findings; the output is what matters

	work := ""

	for line := range strings.Lines(string(out)) {
		if after, ok := strings.CutPrefix(strings.TrimSpace(line), "WORK="); ok {
			work = after
			break
		}
	}

	if work == "" {
		t.Fatalf("no WORK directory in go vet output:\n%s", out)
	}

	t.Cleanup(func() { _ = os.RemoveAll(work) })

	configs, err := filepath.Glob(filepath.Join(work, "*", "vet.cfg"))
	if err != nil {
		t.Fatal(err)
	}

	for _, cfgFile := range configs {
		data, err := os.ReadFile(filepath.Clean(cfgFile))
		if err != nil {
			t.Fatal(err)
		}

		var cfg struct{ ImportPath, VetxOutput string }
		if err := json.Unmarshal(data, &cfg); err != nil {
			t.Fatal(err)
		}

		if cfg.ImportPath == importPath {
			vetx, err := os.ReadFile(filepath.Clean(cfg.VetxOutput))
			if err != nil {
				t.Fatal(err)
			}

			return vetx
		}
	}

	t.Fatalf("go vet did not analyse %s (cache hit?)", importPath)

	return nil
}

func readEvents(t *testing.T, dir string) []trace.Event {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	var events []trace.Event

	for _, name := range files {
		data, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatal(err)
		}

		sc := bufio.NewScanner(bytes.NewReader(data))
		for sc.Scan() {
			var e trace.Event
			if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
				t.Fatal(err)
			}

			events = append(events, e)
		}
	}

	return events
}

func countEvents(t *testing.T, dir, pkg string) int {
	t.Helper()

	n := 0

	for _, e := range readEvents(t, dir) {
		if e.Package == pkg {
			n++
		}
	}

	return n
}
