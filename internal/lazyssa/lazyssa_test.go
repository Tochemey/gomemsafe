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

package lazyssa_test

import (
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/tochemey/gomemsafe/internal/lazyssa"
)

// twice asks for the SSA package twice and reports the SSA function it finds for F.
var twice = &analysis.Analyzer{
	Name:     "lazyssatwice",
	Doc:      "requests SSA twice for tests",
	Requires: []*analysis.Analyzer{lazyssa.Analyzer},
	Run: func(pass *analysis.Pass) (any, error) {
		res := pass.ResultOf[lazyssa.Analyzer].(*lazyssa.Result)

		first := res.Get()
		if res.Get() != first {
			pass.Reportf(pass.Files[0].Package, "Get returned different packages")
		}

		obj := pass.Pkg.Scope().Lookup("F").(*types.Func)
		if fn := res.Func(obj); fn != nil {
			pass.Reportf(obj.Pos(), "built %s", fn.Name())
		}

		return nil, nil
	},
}

// never depends on lazyssa but never asks for SSA.
var never = &analysis.Analyzer{
	Name:     "lazyssanever",
	Doc:      "never requests SSA",
	Requires: []*analysis.Analyzer{lazyssa.Analyzer},
	Run:      func(*analysis.Pass) (any, error) { return nil, nil },
}

func TestBuildsOncePerPackage(t *testing.T) {
	before := lazyssa.Builds()

	analysistest.Run(t, analysistest.TestData(), twice, "p")

	if got := lazyssa.Builds() - before; got != 1 {
		t.Fatalf("Builds grew by %d, want 1", got)
	}
}

func TestBuildsNothingUnlessAsked(t *testing.T) {
	before := lazyssa.Builds()

	analysistest.Run(t, analysistest.TestData(), never, "q")

	if got := lazyssa.Builds() - before; got != 0 {
		t.Fatalf("Builds grew by %d, want 0", got)
	}
}
