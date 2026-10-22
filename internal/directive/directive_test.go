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

package directive_test

import (
	"go/ast"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/tochemey/gomemsafe/internal/directive"
	"github.com/tochemey/gomemsafe/internal/vetmode"
)

// suppressed reports, for the declaration of x in the fixture, which IDs are suppressed on
// its line.
var suppressed = &analysis.Analyzer{
	Name:     "directivesuppressed",
	Doc:      "reports suppressions for tests",
	Requires: []*analysis.Analyzer{directive.Analyzer},
	Run: func(pass *analysis.Pass) (any, error) {
		res := pass.ResultOf[directive.Analyzer].(*directive.Result)

		for _, file := range pass.Files {
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.VAR {
					continue
				}

				pos := gen.Pos()
				pass.Reportf(pos, "MS301=%v MS302=%v MS303=%v",
					res.Suppressed(pass.Fset, pos, "MS301"),
					res.Suppressed(pass.Fset, pos, "MS302"),
					res.Suppressed(pass.Fset, pos, "MS303"))
			}
		}

		return nil, nil
	},
}

func TestReportsMalformedDirectives(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), directive.Analyzer, "d")
}

func TestReportsNothingInFactsOnlyRuns(t *testing.T) {
	vetmode.Set(true)
	t.Cleanup(func() { vetmode.Set(false) })

	results := analysistest.Run(&silentT{t}, analysistest.TestData(), directive.Analyzer, "d")
	for _, r := range results {
		if len(r.Diagnostics) != 0 {
			t.Fatalf("got %d diagnostics in a facts-only run, want 0", len(r.Diagnostics))
		}
	}
}

func TestSuppressedMatchesNamedIDsOnThatLine(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), suppressed, "s")
}

// silentT discards analysistest's complaints about expectations, because in a facts-only run
// the fixture's expected diagnostics are deliberately absent.
type silentT struct{ *testing.T }

func (silentT) Errorf(string, ...any) {}
