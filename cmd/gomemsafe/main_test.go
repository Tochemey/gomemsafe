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
	"encoding/json"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tochemey/gomemsafe/internal/check/lifetime"
	"github.com/tochemey/gomemsafe/internal/check/unsafeflow"
	"github.com/tochemey/gomemsafe/internal/trace"
)

// mainEnv makes a child process of the test binary run main instead of the tests.
const mainEnv = "GOMEMSAFE_TEST_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(mainEnv) != "" {
		main()
	}

	os.Exit(m.Run())
}

func TestVetToolModeDescribesFlags(t *testing.T) {
	out, err := mainCommand(t, "-flags").Output()
	if err != nil {
		t.Fatal(err)
	}

	var flags []struct{ Name string }
	if err := json.Unmarshal(out, &flags); err != nil {
		t.Fatalf("-flags output is not JSON: %v\n%s", err, out)
	}

	// An analyzer and an analyzer option: go vet passes on only the flags listed here.
	safety := unsafeflow.Analyzer.Name + "." + unsafeflow.SafetyFlag
	want := map[string]bool{lifetime.Analyzer.Name: false, safety: false}

	for _, f := range flags {
		if _, ok := want[f.Name]; ok {
			want[f.Name] = true
		}
	}

	for name, found := range want {
		if !found {
			t.Errorf("-flags lacks %s: %s", name, out)
		}
	}
}

func TestVetToolModeReportsVersion(t *testing.T) {
	out, err := mainCommand(t, "-V=full").Output()
	if err != nil {
		t.Fatal(err)
	}

	if f := strings.Fields(string(out)); len(f) < 3 || f[1] != "version" {
		t.Fatalf("-V=full = %q, want the format the go command parses", out)
	}
}

func TestVetToolModeRejectsUnreadableConfig(t *testing.T) {
	cmd := mainCommand(t, filepath.Join(t.TempDir(), "missing.cfg"))

	out, err := cmd.CombinedOutput()

	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(out), "reading vet config") {
		t.Fatalf("err = %v, output %q; want exit 1 and the config error", err, out)
	}
}

func TestFrontEndModeHelp(t *testing.T) {
	cmd := mainCommand(t, "-h")

	out, err := cmd.CombinedOutput()

	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(string(out), "usage: gomemsafe") {
		t.Fatalf("err = %v, output %q; want exit 2 and the usage", err, out)
	}
}

func TestVetToolModeTracesEveryAnalyzer(t *testing.T) {
	module := t.TempDir()
	traces := t.TempDir()

	for name, content := range map[string]string{"go.mod": "module example.com/m\n\ngo 1.26\n", "m.go": "package m\n"} {
		if err := os.WriteFile(filepath.Join(module, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// go vet runs the test binary as its vet tool. A fresh build cache makes it analyse the
	// package rather than replay a cached result.
	cmd := command("go", "vet", "-vettool="+testBinary(t), "./...")
	cmd.Dir = module

	cmd.Env = append(mainEnviron(t), "GOCACHE="+t.TempDir(), "GOFLAGS=", "GOWORK=off", "GOTOOLCHAIN=local",
		"GOPROXY=off", trace.EnvVar+"="+traces)

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go vet: %v\n%s", err, out)
	}

	files, err := filepath.Glob(filepath.Join(traces, "*"+trace.FileExt))
	if err != nil || len(files) == 0 {
		t.Fatalf("trace files = %v, %v; want the vet tool's", files, err)
	}

	data, err := os.ReadFile(filepath.Clean(files[0]))
	if err != nil {
		t.Fatal(err)
	}

	if want := `"analyzer":"` + lifetime.Analyzer.Name + `","kind":"` + trace.KindRun + `"`; !strings.Contains(string(data), want) {
		t.Fatalf("trace lacks %s:\n%s", want, data)
	}
}

// command runs a binary the test chose, with arguments the test chose.
func command(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...) //nolint:gosec // G204: test-controlled binary and arguments
}

// mainCommand runs main with args in a child process of the test binary.
func mainCommand(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()

	cmd := command(testBinary(t), args...)
	cmd.Env = mainEnviron(t)

	return cmd
}

// mainEnviron is the environment in which the test binary runs main. The child writes its
// coverage where the test binary does, so the coverage profile includes main.
func mainEnviron(t *testing.T) []string {
	t.Helper()

	env := append(os.Environ(), mainEnv+"=1")

	if testing.CoverMode() != "" {
		dir := flag.Lookup("test.gocoverdir").Value.String()
		if dir == "" {
			dir = t.TempDir()
		}

		env = append(env, "GOCOVERDIR="+dir)
	}

	return env
}

func testBinary(t *testing.T) string {
	t.Helper()

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	return self
}
