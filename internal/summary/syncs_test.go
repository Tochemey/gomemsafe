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

	"github.com/tochemey/gomemsafe/internal/summary"
)

func TestExportsSyncFacts(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), summary.Analyzer, "syncs", "syncsclient")
}

// TestResultResolvesSyncs resolves synchronisation in the package that declares it, including
// for a function whose name keeps its fact local, and through facts in one that imports it.
func TestResultResolvesSyncs(t *testing.T) {
	results := analysistest.Run(t, analysistest.TestData(), summary.Analyzer, "syncs", "syncsclient")

	for _, r := range results {
		res := r.Result.(*summary.Result)

		want := map[string]bool{"Wait": true, "Plain": false}
		if r.Pass.Pkg.Path() == "syncs" {
			want = map[string]bool{"Receive": true, "Join": true, "waitFor": true, "Through": true, "Done": false, "Plain": false}
		}

		for name, syncs := range want {
			if got := res.Syncs(r.Pass.Pkg.Scope().Lookup(name).(*types.Func)); got != syncs {
				t.Errorf("%s: Syncs(%s) = %v, want %v", r.Pass.Pkg.Path(), name, got, syncs)
			}
		}

		for _, imported := range r.Pass.Pkg.Imports() {
			if imported.Path() != "syncs" {
				continue
			}

			group := types.NewPointer(imported.Scope().Lookup("Group").Type())
			wait, _, _ := types.LookupFieldOrMethod(group, true, imported, "Wait")

			if !res.Syncs(wait.(*types.Func)) || res.Syncs(imported.Scope().Lookup("Plain").(*types.Func)) {
				t.Error("imported facts of syncs do not resolve: Group.Wait must sync, Plain must not")
			}
		}
	}
}
