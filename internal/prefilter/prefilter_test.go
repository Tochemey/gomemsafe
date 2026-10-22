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
	"fmt"
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/tochemey/gomemsafe/internal/prefilter"
)

// reporter reports, at each function declaration, the static callees the prefilter recorded,
// so the fixture's want comments check them.
var reporter = &analysis.Analyzer{
	Name:     "prefilterreporter",
	Doc:      "reports the prefilter's callees for tests",
	Requires: []*analysis.Analyzer{prefilter.Analyzer},
	Run: func(pass *analysis.Pass) (any, error) {
		res := pass.ResultOf[prefilter.Analyzer].(*prefilter.Result)
		for _, f := range res.Funcs {
			names := make([]string, 0, len(f.Callees))
			for _, c := range f.Callees {
				names = append(names, calleeName(c))
			}

			pass.Reportf(f.Decl.Pos(), "callees: %s", strings.Join(names, ", "))
		}

		for pos, call := range res.Calls {
			if pos != call.Lparen {
				return nil, fmt.Errorf("call indexed at %v, want its Lparen %v", pos, call.Lparen)
			}
		}

		return nil, nil
	},
}

func TestRecordsStaticCalleesPerFunction(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), reporter, "p")
}

func calleeName(fn *types.Func) string {
	if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
		return fn.FullName()
	}

	return fn.Pkg().Name() + "." + fn.Name()
}
