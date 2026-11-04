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

package prefilter_test

import (
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/tochemey/gomemsafe/internal/prefilter"
)

// escapeReporter reports, at each function declaration that names a parameter where its
// memory could leave, how: stored by the function itself, passed to a dynamic callee, or
// passed to the static callees listed, marked when the call's result is used.
var escapeReporter = &analysis.Analyzer{
	Name:     "prefilterescapes",
	Doc:      "reports the prefilter's parameter escapes for tests",
	Requires: []*analysis.Analyzer{prefilter.Analyzer},
	Run: func(pass *analysis.Pass) (any, error) {
		for _, f := range pass.ResultOf[prefilter.Analyzer].(*prefilter.Result).Funcs {
			var parts []string

			if f.ParamStored {
				parts = append(parts, "stored")
			}

			if f.ParamDynamic {
				parts = append(parts, "dynamic")
			}

			for _, c := range f.ArgCallees {
				name := calleeName(c)
				if slices.Contains(f.ResultCallees, c) {
					name += " (result used)"
				}

				parts = append(parts, name)
			}

			if len(parts) > 0 {
				pass.Reportf(f.Decl.Pos(), "escapes: %s", strings.Join(parts, ", "))
			}
		}

		return nil, nil
	},
}

func TestRecordsWhereParametersMayEscape(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), escapeReporter, "escapes")
}
