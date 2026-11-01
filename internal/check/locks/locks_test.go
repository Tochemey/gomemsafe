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

package locks

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/tochemey/gomemsafe/internal/vetmode"
)

func TestReportsBrokenLockContracts(t *testing.T) {
	results := analysistest.Run(t, analysistest.TestData(), Analyzer, "guarded")
	for _, r := range results {
		for _, d := range r.Diagnostics {
			if d.Category != ID.String() || len(d.SuggestedFixes) != 1 {
				t.Errorf("%q: category %q, %d fixes; want %s and 1", d.Message, d.Category, len(d.SuggestedFixes), ID)
			}

			if strings.Contains(d.Message, "without holding") && !strings.Contains(d.Message, "called") &&
				!strings.Contains(d.Message, "returns") && len(d.Related) != 1 {
				t.Errorf("%q: %d related, want the guard's declaration", d.Message, len(d.Related))
			}
		}
	}
}

func TestReportsBrokenContractsAcrossPackages(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "guardedclient")
}

func TestStaysSilentOnHonouredContracts(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "guardedfalsepos")
}

func TestHonoursSuppressionsForcesAndIgnores(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "suppress")
}

func TestReportsNothingInFactsOnlyRuns(t *testing.T) {
	vetmode.Set(true)
	t.Cleanup(func() { vetmode.Set(false) })

	for _, r := range analysistest.Run(&silentT{t}, analysistest.TestData(), Analyzer, "guarded") {
		if len(r.Diagnostics) != 0 {
			t.Fatalf("%d diagnostics in a facts-only run, want 0", len(r.Diagnostics))
		}
	}
}

// TestReportsNothingOverBudget lowers the dataflow budget so that every function, and every
// literal inlined in one, is abandoned: a function over budget reports nothing.
func TestReportsNothingOverBudget(t *testing.T) {
	budget = 1

	t.Cleanup(func() { budget = 0 })

	for _, r := range analysistest.Run(&silentT{t}, analysistest.TestData(), Analyzer, "guarded") {
		if len(r.Diagnostics) != 0 {
			t.Fatalf("%d diagnostics over budget, want 0", len(r.Diagnostics))
		}
	}
}

// silentT discards analysistest's complaints about expectations, because the fixture's
// expected diagnostics are deliberately absent.
type silentT struct{ *testing.T }

func (silentT) Errorf(string, ...any) {}
