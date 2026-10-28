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

package sharing

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/lazyssa"
	"github.com/tochemey/gomemsafe/internal/vetmode"
)

func TestReportsUnsynchronizedCaptures(t *testing.T) {
	results := analysistest.Run(t, analysistest.TestData(), Analyzer, "raced")
	for _, r := range results {
		for _, d := range r.Diagnostics {
			if d.Category != CaptureID.String() || len(d.Related) != 2 || len(d.SuggestedFixes) != 1 {
				t.Errorf("%q: category %q, %d related, %d fixes; want %s, 2, and 1",
					d.Message, d.Category, len(d.Related), len(d.SuggestedFixes), CaptureID)
			}
		}
	}
}

func TestStaysSilentOnSynchronizedCaptures(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "racedfalsepos")
}

func TestHonoursSuppressions(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "suppress")
}

func TestSkipsFactsOnlyRuns(t *testing.T) {
	vetmode.Set(true)
	t.Cleanup(func() { vetmode.Set(false) })

	analysistest.Run(t, analysistest.TestData(), Analyzer, "unreported")
}

// TestReportsNothingOverBudget covers a race in a function whose dataflow exceeds the budget:
// the result is unknown, so the check fails quiet instead of reporting.
func TestReportsNothingOverBudget(t *testing.T) {
	budget = 1

	t.Cleanup(func() { budget = 0 })

	analysistest.Run(t, analysistest.TestData(), Analyzer, "unreported")
}

func TestBuildsNoSSAWithoutGoroutines(t *testing.T) {
	before := lazyssa.Builds()

	analysistest.Run(t, analysistest.TestData(), Analyzer, "plain")

	if got := lazyssa.Builds() - before; got != 0 {
		t.Fatalf("built SSA for %d packages, want 0", got)
	}
}

// TestVariableNameFollowsFields names a field path into a struct variable, a free variable, and
// a path longer than the variable's type allows, which stops at the last field.
func TestVariableNameFollowsFields(t *testing.T) {
	pkg := buildSSA(t, racesSrc)
	o := instrOf[*ssa.Alloc](t, pkg.Func("fields"))

	for _, tc := range []struct {
		path []int
		want string
	}{
		{nil, "o"},
		{[]int{0, 1}, "o.in.b"},
		{[]int{1}, "o.c"},
		{[]int{1, 0}, "o.c"},
		{[]int{7}, "o"},
	} {
		if got := variableName(o, tc.path); got != tc.want {
			t.Errorf("variableName(o, %v) = %q, want %q", tc.path, got, tc.want)
		}
	}

	fv := pkg.Func("fromLiteral").AnonFuncs[0].FreeVars[0]
	if got := variableName(fv, nil); got != "x" {
		t.Errorf("variableName of a free variable = %q, want x", got)
	}
}
