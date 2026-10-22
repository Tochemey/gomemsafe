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

package report_test

import (
	"go/ast"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/tochemey/gomemsafe/internal/directive"
	"github.com/tochemey/gomemsafe/internal/report"
)

// everyVar reports an MS301 finding at every variable declaration through report.Report.
var everyVar = &analysis.Analyzer{
	Name:     "reporteveryvar",
	Doc:      "reports every var for tests",
	Requires: []*analysis.Analyzer{directive.Analyzer},
	Run: func(pass *analysis.Pass) (any, error) {
		for _, file := range pass.Files {
			for _, decl := range file.Decls {
				if gen, ok := decl.(*ast.GenDecl); ok {
					report.Report(pass, analysis.Diagnostic{Pos: gen.Pos(), Category: "MS301", Message: "MS301: finding"})
				}
			}
		}

		return nil, nil
	},
}

func TestReportHonoursSuppressions(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), everyVar, "r")
}
