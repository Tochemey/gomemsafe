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

// Command rsswrap is a go build -toolexec wrapper that records the peak resident set size of
// every compile. The benchmark (internal/cmd/bench) compares it with gomemsafe's own peak RSS
// for the same package, which must stay within twice the compile's.
//
// It runs the tool it is given, passes its exit code through, and, when the tool is the
// compiler and GOMEMSAFE_COMPILE_RSS (trace.CompileRSSEnvVar) names a file, appends one
// "<package>\t<bytes>" line to it.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/tochemey/gomemsafe/internal/trace"
)

const (
	// compilerTool is the base name of the tool whose memory is measured.
	compilerTool = "compile"
	// packageFlag precedes the import path in the compiler's arguments.
	packageFlag = "-p"
	// exeSuffix ends tool names on Windows.
	exeSuffix = ".exe"
	// usageExit is the exit code when no tool is given.
	usageExit = 2
)

func main() {
	os.Exit(wrap(os.Args[1:], os.Getenv(trace.CompileRSSEnvVar)))
}

// wrap runs args[0] with the remaining arguments and returns its exit code.
func wrap(args []string, out string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: rsswrap tool [args...]")
		return usageExit
	}

	cmd := exec.Command(args[0], args[1:]...) //nolint:gosec // G204: the go command's own tool invocation
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	err := cmd.Run()

	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		fmt.Fprintln(os.Stderr, "rsswrap:", err)
		return 1
	}

	if out != "" && isCompiler(args[0]) && cmd.ProcessState != nil {
		if err := record(out, packagePath(args[1:]), maxRSS(cmd.ProcessState)); err != nil {
			fmt.Fprintln(os.Stderr, "rsswrap:", err)
		}
	}

	return cmd.ProcessState.ExitCode()
}

func isCompiler(tool string) bool {
	return strings.TrimSuffix(filepath.Base(tool), exeSuffix) == compilerTool
}

// packagePath returns the value of the compiler's -p flag.
func packagePath(args []string) string {
	for i, a := range args {
		if a == packageFlag && i+1 < len(args) {
			return args[i+1]
		}
	}

	return ""
}

// maxRSS returns the peak resident set size of a finished process in bytes. On Linux it is at
// least rsswrap's own peak, which the kernel carries into the compile across exec; rsswrap is
// small, so a compile's figure is accurate except for the smallest packages.
func maxRSS(ps *os.ProcessState) int64 {
	ru, ok := ps.SysUsage().(*syscall.Rusage)
	if !ok {
		return 0
	}

	return trace.RusageMaxRSS(ru)
}

// record appends one measurement. Parallel compiles append concurrently; each line is a
// single small write to a file opened with O_APPEND, so lines do not interleave.
func record(out, pkg string, rss int64) error {
	// out comes from trace.CompileRSSEnvVar, set by whoever runs go build; writing there is the
	// point.
	f, err := os.OpenFile(filepath.Clean(out), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // G703: see above
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(f, "%s\t%d\n", pkg, rss); err != nil {
		_ = f.Close()
		return err
	}

	return f.Close()
}
