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

//go:build unix

package main

import (
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mainEnv makes a child process of the test binary run main instead of the tests.
const mainEnv = "GOMEMSAFE_TEST_MAIN"

func TestMain(m *testing.M) {
	if os.Getenv(mainEnv) != "" {
		main()
	}

	os.Exit(m.Run())
}

func TestMainPassesTheToolsExitCode(t *testing.T) {
	err := mainCommand(t, "sh", "-c", "exit 3").Run()

	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("err = %v, want exit status 3 from the tool", err)
	}
}

func TestPackagePath(t *testing.T) {
	if got := packagePath([]string{"-o", "x.a", "-p", "example.com/p", "a.go"}); got != "example.com/p" {
		t.Fatalf("packagePath = %q", got)
	}

	if got := packagePath([]string{"-p"}); got != "" {
		t.Fatalf("trailing -p: %q", got)
	}
}

func TestIsCompiler(t *testing.T) {
	if !isCompiler("/go/pkg/tool/linux_amd64/compile") || !isCompiler(`compile.exe`) || isCompiler("/x/asm") {
		t.Fatal("isCompiler misclassifies tools")
	}
}

func TestWrapRecordsCompilesAndPassesExitCodes(t *testing.T) {
	dir := t.TempDir()
	compile := filepath.Join(dir, "compile")

	if err := os.WriteFile(compile, []byte("#!/bin/sh\nexit 3\n"), 0o700); err != nil { //nolint:gosec // G306: the fake tool must be executable
		t.Fatal(err)
	}

	out := filepath.Join(dir, "rss.tsv")

	if code := wrap([]string{compile, "-p", "example.com/p"}, out); code != 3 {
		t.Fatalf("exit code = %d, want the tool's 3", code)
	}

	data, err := os.ReadFile(filepath.Clean(out))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(string(data), "example.com/p\t") {
		t.Fatalf("recorded %q", data)
	}

	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Fatal(err)
	}

	if code := wrap([]string{truePath}, out); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	if after, _ := os.ReadFile(filepath.Clean(out)); string(after) != string(data) {
		t.Fatal("a tool other than the compiler was recorded")
	}
}

func TestWrapKeepsTheExitCodeWhenRecordingFails(t *testing.T) {
	dir := t.TempDir()
	compile := filepath.Join(dir, "compile")

	if err := os.WriteFile(compile, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil { //nolint:gosec // G306: the fake tool must be executable
		t.Fatal(err)
	}

	if code := wrap([]string{compile, "-p", "p"}, filepath.Join(dir, "missing", "rss.tsv")); code != 0 {
		t.Fatalf("exit code = %d, want the tool's 0 even when the output cannot be written", code)
	}
}

func TestWrapRejectsMissingTool(t *testing.T) {
	if code := wrap(nil, ""); code != usageExit {
		t.Fatalf("no tool: exit %d, want %d", code, usageExit)
	}

	if code := wrap([]string{filepath.Join(t.TempDir(), "missing")}, ""); code != 1 {
		t.Fatalf("missing tool: exit %d, want 1", code)
	}
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
