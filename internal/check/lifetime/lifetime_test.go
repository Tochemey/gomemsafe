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

package lifetime

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/invalidation"
	"github.com/tochemey/gomemsafe/internal/lazyssa"
	"github.com/tochemey/gomemsafe/internal/vetmode"
)

func TestReportsUseAfterPut(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "basic")
}

func TestStaysSilentOnCorrectCode(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "falsepos")
}

// TestPointsAtTheValue checks that each diagnostic's column is where the used value is
// written, even when SSA places the use at an operator such as "." or "==".
func TestPointsAtTheValue(t *testing.T) {
	name := regexp.MustCompile(`^MS301: (\w+) used`)

	results := analysistest.Run(t, analysistest.TestData(), Analyzer, "position")
	for _, r := range results {
		for _, d := range r.Diagnostics {
			m := name.FindStringSubmatch(d.Message)
			if m == nil {
				t.Fatalf("unexpected message %q", d.Message)
			}

			posn := r.Pass.Fset.Position(d.Pos)
			if got := sourceAt(t, posn); !strings.HasPrefix(got, m[1]) {
				t.Errorf("%s: points at %q, want the value %s", posn, got, m[1])
			}
		}
	}
}

func TestHonoursSuppressions(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "suppress")
}

func TestUsesSummariesAcrossPackages(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "user")
}

func TestSkipsFactsOnlyRuns(t *testing.T) {
	vetmode.Set(true)
	t.Cleanup(func() { vetmode.Set(false) })

	analysistest.Run(t, analysistest.TestData(), Analyzer, "skip")
}

func TestBuildsNoSSAWithoutTriggers(t *testing.T) {
	before := lazyssa.Builds()

	analysistest.Run(t, analysistest.TestData(), Analyzer, "clean")

	if got := lazyssa.Builds() - before; got != 0 {
		t.Fatalf("built SSA for %d packages, want 0 for a package without invalidating calls", got)
	}
}

// TestFallsBackForCallsWithoutSource covers a call SSA gives no source call for: no operand or
// argument is found, and the callee is named from its object.
func TestFallsBackForCallsWithoutSource(t *testing.T) {
	sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	site := invalidation.Site{Call: &ssa.Call{}, Callee: types.NewFunc(token.NoPos, nil, "release", sig)}
	c := &checker{pass: &analysis.Pass{}, calls: make(map[token.Pos]*ast.CallExpr)}

	if expr := c.callOperand(site.Call, nil); expr != nil {
		t.Errorf("callOperand = %v, want nil", expr)
	}

	if expr := c.operandAt(site.Call, nil, site.Call.Pos()); expr != nil {
		t.Errorf("operandAt = %v, want nil", expr)
	}

	if arg := c.siteArg(site, 0); arg != nil {
		t.Errorf("siteArg = %v, want nil", arg)
	}

	if got := c.funText(site); got != "release" {
		t.Errorf("funText = %q, want the callee's name", got)
	}

	if got, text := valueName(nil), exprText(nil); got != unknownValueText || text != unknownValueText {
		t.Errorf("valueName, exprText of no argument = %q, %q; want %q", got, text, unknownValueText)
	}
}

// TestFindsNoSourceForArgumentsSSAAdds covers a call with fewer source arguments than SSA
// ones, such as release(pair()) with pair returning two values.
func TestFindsNoSourceForArgumentsSSAAdds(t *testing.T) {
	call := &ast.CallExpr{Fun: ast.NewIdent("release")}
	site := invalidation.Site{Call: &ssa.Call{}}
	c := &checker{calls: map[token.Pos]*ast.CallExpr{site.Call.Pos(): call}}

	if arg := c.siteArg(site, 1); arg != nil {
		t.Errorf("siteArg = %v, want nil", arg)
	}

	v := ssa.NewConst(constant.MakeInt64(1), types.Typ[types.Int])
	if expr := argExpr(call, []ssa.Value{v}, v, 0); expr != nil {
		t.Errorf("argExpr = %v, want nil", expr)
	}
}

// sourceAt returns the source text from posn to the end of its line.
func sourceAt(t *testing.T, posn token.Position) string {
	t.Helper()

	src, err := os.ReadFile(posn.Filename)
	if err != nil {
		t.Fatal(err)
	}

	return strings.Split(string(src), "\n")[posn.Line-1][posn.Column-1:]
}
