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

package ownership

import (
	"go/ast"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/lazyssa"
	"github.com/tochemey/gomemsafe/internal/vetmode"
)

func TestReportsUsesAfterMove(t *testing.T) {
	results := analysistest.Run(t, analysistest.TestData(), Analyzer, "moved")
	for _, r := range results {
		for _, d := range r.Diagnostics {
			if d.Category != MoveID.String() || len(d.Related) != 1 || len(d.SuggestedFixes) != 1 {
				t.Errorf("%q: category %q, %d related, %d fixes; want %s, 1, and 1",
					d.Message, d.Category, len(d.Related), len(d.SuggestedFixes), MoveID)
			}
		}
	}
}

func TestStaysSilentOnValuesStillOwned(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "movedfalsepos")
}

func TestHonoursSuppressions(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "suppress")
}

func TestSkipsFactsOnlyRuns(t *testing.T) {
	vetmode.Set(true)
	t.Cleanup(func() { vetmode.Set(false) })

	analysistest.Run(t, analysistest.TestData(), Analyzer, "plain")
}

// TestBuildsNoSSAWithoutAnnotations covers a package that sends and stores pointers but
// annotates nothing, and one whose sends name no owned value: MS501 has nothing to follow.
func TestBuildsNoSSAWithoutAnnotations(t *testing.T) {
	before := lazyssa.Builds()

	analysistest.Run(t, analysistest.TestData(), Analyzer, "plain", "ownedunused")

	if got := lazyssa.Builds() - before; got != 0 {
		t.Fatalf("built SSA for %d packages, want 0", got)
	}
}

// TestNamesWhatIsNotInTheSource covers a consuming call that the call index does not hold, and
// a use whose source expression is not found.
func TestNamesWhatIsNotInTheSource(t *testing.T) {
	call := instrOf[*ssa.Call](t, buildMoves(t).Func("sel"))
	c := &checker{calls: make(map[token.Pos]*ast.CallExpr)}

	if got := c.calleeText(call); got != "use" {
		t.Errorf("calleeText = %q, want the callee's name", got)
	}

	if got := valueName(nil); got != unknownValueText {
		t.Errorf("valueName(nil) = %q, want %q", got, unknownValueText)
	}
}
