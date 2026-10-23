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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tochemey/gomemsafe/internal/check/lifetime"
	"github.com/tochemey/gomemsafe/internal/check/unsafeflow"
)

func TestVetToolModeDescribesFlags(t *testing.T) {
	out, err := command(build(t), "-flags").Output()
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
	out, err := command(build(t), "-V=full").Output()
	if err != nil {
		t.Fatal(err)
	}

	if f := strings.Fields(string(out)); len(f) < 3 || f[1] != "version" {
		t.Fatalf("-V=full = %q, want the format the go command parses", out)
	}
}

func TestVetToolModeRejectsUnreadableConfig(t *testing.T) {
	cmd := command(build(t), filepath.Join(t.TempDir(), "missing.cfg"))

	out, err := cmd.CombinedOutput()

	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(out), "reading vet config") {
		t.Fatalf("err = %v, output %q; want exit 1 and the config error", err, out)
	}
}

func TestFrontEndModeHelp(t *testing.T) {
	cmd := command(build(t), "-h")

	out, err := cmd.CombinedOutput()

	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(string(out), "usage: gomemsafe") {
		t.Fatalf("err = %v, output %q; want exit 2 and the usage", err, out)
	}
}

// command runs a binary the test built, or the go command, with arguments the test chose.
func command(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...) //nolint:gosec // G204: test-controlled binary and arguments
}

// build compiles this command into a temporary directory and returns the binary's path.
func build(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "gomemsafe")

	out, err := command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	return bin
}
