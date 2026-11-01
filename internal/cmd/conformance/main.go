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

// Command conformance checks that MS403 gives the same verdicts as gVisor's checklocks on
// checklocks' own test cases, as decision D6 requires.
//
// It fetches the pinned gVisor commit with go mod download, which verifies its checksum,
// copies the checklocks test package into a temporary module, builds the checklocks tool from
// the pinned module-compatible branch, and runs it: it must report nothing, which shows the
// test cases' expectations hold. It then runs gomemsafe, built from the working tree, and
// compares, file by file, the lines carrying checklocks' +checklocksfail marker with the lines
// where gomemsafe reports a broken lock contract. On a compared file the two sets must be
// equal; on a context file, one whose annotations gomemsafe does not support yet, the
// agreement is only shown. The results table goes to standard output and, with -out, to a
// file. The run leaves nothing behind.
//
// Nothing of gVisor enters the repository: the corpus and the tool live in a temporary
// directory for the length of the run.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tochemey/gomemsafe/internal/frontend"
)

// The pinned corpus and tool. The tests live on gVisor's master branch; the tool is built
// from the go branch, the module-compatible one, which also provides the packages the tests
// import. To move a pin, change both together and commit.
const (
	corpusModule  = "gvisor.dev/gvisor"
	corpusVersion = "v0.0.0-20261031105741-aa767e4c43d0"
	toolVersion   = "v0.0.0-20261031110703-16342efb9a78"
	testDir       = "tools/checklocks/test"
	checklocksCmd = "gvisor.dev/gvisor/tools/checklocks/cmd/checklocks"
	// corpusModulePath keeps the test package's import paths valid: the test package and its
	// subpackages resolve inside the temporary module, and the rest of gVisor from the pin.
	corpusModulePath = "gvisor.dev/gvisor/tools/checklocks/test"
	corpusGoMod      = "module " + corpusModulePath + "\n\ngo 1.26.0\n\nrequire " + corpusModule + " " + toolVersion + "\n"
)

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// options are the command's flags.
type options struct {
	out string
}

// runner runs a command in dir and returns its combined output. Tests replace it.
type runner func(ctx context.Context, dir, name string, args ...string) ([]byte, error)

// download is what go mod download -json prints for one module.
type download struct {
	Dir string `json:"Dir"`
}

func main() {
	os.Exit(runConformance(os.Args[1:], os.Stdout, os.Stderr, commandOutput))
}

// runConformance runs the comparison and returns the exit code.
func runConformance(args []string, stdout, stderr io.Writer, run runner) int {
	o, err := parseOptions(args, stderr)
	if err != nil {
		return exitUsage
	}

	if err := conform(context.Background(), o, stdout, run); err != nil {
		_, _ = fmt.Fprintln(stderr, "conformance:", err)
		return exitError
	}

	return exitOK
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	var o options

	fs := flag.NewFlagSet("conformance", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.out, "out", "", "also write the results table, as markdown, to this file")

	if err := fs.Parse(args); err != nil {
		return o, err
	}

	if fs.NArg() > 0 {
		_, _ = fmt.Fprintln(stderr, "conformance takes no arguments")
		return o, errors.New("unexpected arguments")
	}

	return o, nil
}

// conform fetches the corpus, runs both tools on it, and compares their verdicts.
func conform(ctx context.Context, o options, stdout io.Writer, run runner) error {
	root, err := moduleRoot(ctx, run)
	if err != nil {
		return err
	}

	work, err := os.MkdirTemp("", "gomemsafe-conformance-*")
	if err != nil {
		return err
	}

	defer func() { _ = os.RemoveAll(work) }()

	corpus := filepath.Join(work, "corpus")
	if err := prepareCorpus(ctx, corpus, run); err != nil {
		return err
	}

	checklocks, gomemsafe := filepath.Join(work, "checklocks"), filepath.Join(work, "gomemsafe")

	for _, build := range []struct {
		dir, out, pkg string
	}{{corpus, checklocks, checklocksCmd}, {root, gomemsafe, "./cmd/gomemsafe"}} {
		if out, err := run(ctx, build.dir, "go", "build", "-o", build.out, build.pkg); err != nil {
			return fmt.Errorf("building %s: %w\n%s", build.pkg, err, out)
		}
	}

	reference, err := vet(ctx, corpus, checklocks, run)
	if err != nil {
		return err
	}

	if len(reference) > 0 {
		return fmt.Errorf("checklocks does not pass its own tests at %s; the pin is inconsistent:\n%s",
			corpusVersion, describe(reference, corpus))
	}

	diags, err := vet(ctx, corpus, gomemsafe, run)
	if err != nil {
		return err
	}

	expected, err := readExpectations(corpus)
	if err != nil {
		return err
	}

	results := compare(expected, verdicts(diags, corpus))
	table := markdown(results)

	if _, err := fmt.Fprint(stdout, table); err != nil {
		return err
	}

	if o.out != "" {
		if err := os.WriteFile(o.out, []byte(table), 0o600); err != nil {
			return err
		}
	}

	if disagreeing := disagreements(results); len(disagreeing) > 0 {
		return fmt.Errorf("verdicts differ from checklocks on %s", strings.Join(disagreeing, ", "))
	}

	return nil
}

// moduleRoot returns the directory of the module the command runs in.
func moduleRoot(ctx context.Context, run runner) (string, error) {
	out, err := run(ctx, "", "go", "env", "GOMOD")
	if err != nil {
		return "", fmt.Errorf("finding the module: %w", err)
	}

	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == os.DevNull {
		return "", errors.New("conformance must run inside the gomemsafe module")
	}

	return filepath.Dir(gomod), nil
}

// prepareCorpus makes dir a module holding checklocks' test package at the pinned commit, with
// the pinned gVisor branch and the checklocks tool's dependencies resolved.
func prepareCorpus(ctx context.Context, dir string, run runner) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(corpusGoMod), 0o600); err != nil {
		return err
	}

	out, err := run(ctx, dir, "go", "mod", "download", "-json", corpusModule+"@"+corpusVersion)
	if err != nil {
		return fmt.Errorf("downloading %s@%s: %w\n%s", corpusModule, corpusVersion, err, out)
	}

	var d download
	if err := json.Unmarshal(out, &d); err != nil || d.Dir == "" {
		return fmt.Errorf("reading go mod download's output: %w\n%s", err, out)
	}

	if err := copyGoFiles(filepath.Join(d.Dir, testDir), dir); err != nil {
		return err
	}

	for _, args := range [][]string{
		{"mod", "tidy"},
		{"get", checklocksCmd + "@" + toolVersion},
	} {
		if out, err := run(ctx, dir, "go", args...); err != nil {
			return fmt.Errorf("go %s in the corpus: %w\n%s", strings.Join(args, " "), err, out)
		}
	}

	return nil
}

// copyGoFiles copies the Go files under src to dst, keeping the directory layout. The module
// cache is read-only, so the copies are writable.
func copyGoFiles(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		target := filepath.Join(dst, rel)

		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o750)
		case !strings.HasSuffix(d.Name(), ".go"):
			return nil
		}

		data, err := os.ReadFile(path) //nolint:gosec // G304: a file of the downloaded, checksum-verified corpus
		if err != nil {
			return err
		}

		return os.WriteFile(target, data, 0o600) //nolint:gosec // G703: target is under the temporary corpus directory
	})
}

// vet runs tool as the vet tool on every package of dir and returns its diagnostics. go vet
// exits non-zero when the tool reports, so only an exit without output is a failure.
func vet(ctx context.Context, dir, tool string, run runner) ([]frontend.Diagnostic, error) {
	out, err := run(ctx, dir, "go", "vet", "-vettool="+tool, "-json", "./...")

	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || len(out) == 0) {
		return nil, fmt.Errorf("running %s: %w\n%s", filepath.Base(tool), err, out)
	}

	diags, failures, err := frontend.Decode(out)
	if err != nil {
		return nil, fmt.Errorf("reading %s's output: %w", filepath.Base(tool), err)
	}

	if len(failures) > 0 {
		return nil, fmt.Errorf("%s failed:\n%s", filepath.Base(tool), strings.Join(failures, "\n"))
	}

	return diags, nil
}

// readExpectations reads the +checklocksfail lines of every corpus file.
func readExpectations(corpus string) (map[string][]int, error) {
	expected := make(map[string][]int)

	for _, f := range corpusFiles {
		src, err := os.ReadFile(filepath.Join(corpus, f.name)) //nolint:gosec // G304: a file of the corpus this command copied
		if err != nil {
			return nil, err
		}

		expected[f.name] = expectations(src)
	}

	return expected, nil
}

// describe lists diagnostics, by position relative to dir, for an error message.
func describe(diags []frontend.Diagnostic, dir string) string {
	lines := make([]string, len(diags))
	for i, d := range diags {
		lines[i] = strings.TrimPrefix(d.Posn, dir+string(filepath.Separator)) + ": " + d.Message
	}

	return strings.Join(lines, "\n")
}

// commandOutput runs name with args in dir and returns its combined output.
func commandOutput(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: go with arguments this command chooses
	cmd.Dir = dir

	return cmd.CombinedOutput()
}
