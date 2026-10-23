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

// Package lazyssa builds a package's SSA form on first request, at most once, and shares it
// between the analyzers that need it. Packages nobody asks about never pay for SSA, which is
// the largest single cost of the analysis.
//
// It mirrors buildssa, without buildssa's dependency on ctrlflow and without eagerly building
// every package. Builds counts constructions so tests can prove the laziness.
package lazyssa

import (
	"go/types"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/trace"
)

// Analyzer returns a *Result whose Get method builds the SSA package.
var Analyzer = &analysis.Analyzer{
	Name:       "lazyssa",
	Doc:        "builds SSA for the current package on demand",
	Run:        run,
	ResultType: reflect.TypeFor[*Result](),
}

// builds counts SSA constructions in this process; tests use it to prove laziness.
var builds atomic.Int64

// Result gives access to the SSA form of the current package.
type Result struct {
	pass *analysis.Pass
	once sync.Once
	pkg  *ssa.Package
}

// Builds returns how many packages have been lowered to SSA in this process.
func Builds() int64 { return builds.Load() }

// Get builds the SSA package on the first call and returns the same package afterwards.
func (r *Result) Get() *ssa.Package {
	r.once.Do(func() {
		start := time.Now()

		builds.Add(1)

		prog := ssa.NewProgram(r.pass.Fset, ssa.BuilderMode(0))
		for _, p := range r.pass.Pkg.Imports() {
			prog.CreatePackage(p, nil, nil, true)
		}

		r.pkg = prog.CreatePackage(r.pass.Pkg, r.pass.Files, r.pass.TypesInfo, false)
		r.pkg.Build()
		trace.Record(r.pass.Pkg.Path(), Analyzer.Name, trace.KindSSA, time.Since(start))
	})

	return r.pkg
}

// Func returns the SSA function for a function declared in the current package.
func (r *Result) Func(obj *types.Func) *ssa.Function {
	return r.Get().Prog.FuncValue(obj)
}

func run(pass *analysis.Pass) (any, error) {
	return &Result{pass: pass}, nil
}
