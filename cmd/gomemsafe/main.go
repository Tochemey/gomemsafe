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

// Command gomemsafe checks Go code for memory-safety hazards at build time.
//
//	go tool gomemsafe [-json] [-stats] [analyzer flags] [packages]
//
// The binary has two modes, chosen by its arguments (vetmode.IsVetToolInvocation):
//
//   - Front-end mode, what users run: it runs `go vet -vettool=<itself> -json`, renders
//     Rust-style diagnostics, and exits 0 when clean, 1 on findings, and 2 on errors
//     (package frontend).
//   - Vet-tool mode, what the go command runs once per package: it records whether the run
//     only computes facts (package vetmode) and hands over to unitchecker.Main with the
//     analyzers of package analyzers. With GOMEMSAFE_TRACE set, every analyzer is timed
//     (package trace).
package main

import (
	"fmt"
	"os"

	"golang.org/x/tools/go/analysis/unitchecker"

	"github.com/tochemey/gomemsafe/analyzers"
	"github.com/tochemey/gomemsafe/internal/frontend"
	"github.com/tochemey/gomemsafe/internal/trace"
	"github.com/tochemey/gomemsafe/internal/vetmode"
)

func main() {
	args := os.Args[1:]
	if !vetmode.IsVetToolInvocation(args) {
		os.Exit(frontend.Main(args, os.Stdout, os.Stderr))
	}

	if err := vetmode.Detect(args); err != nil {
		fmt.Fprintln(os.Stderr, frontend.ProgramName+":", err)
		os.Exit(1)
	}

	all := analyzers.All()
	if trace.Enabled() {
		trace.Wrap(all)
	}

	unitchecker.Main(all...)
}
