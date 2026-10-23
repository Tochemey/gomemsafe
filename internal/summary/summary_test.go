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

package summary_test

import (
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/tochemey/gomemsafe/internal/effect"
	"github.com/tochemey/gomemsafe/internal/summary"
)

func TestExportsInvalidationFacts(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), summary.Analyzer, "helper")
}

func TestResultResolvesCallees(t *testing.T) {
	r := analysistest.Run(t, analysistest.TestData(), summary.Analyzer, "helper")[0]
	res := r.Result.(*summary.Result)

	for name, want := range map[string]effect.ArgSet{"Release": 0b1, "MaybeRelease": 0} {
		fn := r.Pass.Pkg.Scope().Lookup(name).(*types.Func)
		if got := res.Invalidates(fn); got != want {
			t.Errorf("Invalidates(%s) = %#x, want %#x", name, uint64(got), uint64(want))
		}
	}
}

func TestFactString(t *testing.T) {
	f := summary.Fact{Invalidates: effect.ArgSet(0b101)}

	if got := f.String(); got != "invalidates(0x5)" {
		t.Fatalf("String() = %q", got)
	}
}
