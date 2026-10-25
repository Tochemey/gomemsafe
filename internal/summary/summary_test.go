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

func TestExportsViewFacts(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), summary.Analyzer, "views")
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

// TestResultResolvesViews resolves the views and events of the views fixture through the
// Result, as checks do: from facts and contracts, and from known effects for the standard
// library, which take precedence.
func TestResultResolvesViews(t *testing.T) {
	r := analysistest.Run(t, analysistest.TestData(), summary.Analyzer, "views")[0]
	res := r.Result.(*summary.Result)
	scope := r.Pass.Pkg.Scope()

	second := scope.Lookup("Second").(*types.Func)
	if view, ok := res.ViewOf(second); !ok || view != (effect.View{Arg: 1, EndedBy: effect.Read | effect.Write | effect.Clear}) {
		t.Errorf("ViewOf(Second) = %+v, %v", view, ok)
	}

	if _, ok := res.ViewOf(scope.Lookup("Copy").(*types.Func)); ok {
		t.Error("ViewOf(Copy) found a view; Copy returns a copy")
	}

	fill := scope.Lookup("Fill").(*types.Func)
	if got := res.EventsOn(fill, 1); got != effect.Write || res.EventsOn(fill, 0) != 0 || res.EventsOn(fill, 2) != 0 {
		t.Errorf("EventsOn(Fill) = %b on argument 1, want only a write there", got)
	}

	if !res.EndsViews(fill) || res.EndsViews(scope.Lookup("MaybeClear").(*types.Func)) {
		t.Error("EndsViews is wrong for Fill or MaybeClear")
	}

	if res.Lookup() != res {
		t.Error("Lookup is not the Result itself")
	}

	var bytesPkg *types.Package

	for _, imported := range r.Pass.Pkg.Imports() {
		if imported.Path() == "bytes" {
			bytesPkg = imported
		}
	}

	buffer := types.NewPointer(bytesPkg.Scope().Lookup("Buffer").Type())
	method := func(name string) *types.Func {
		obj, _, _ := types.LookupFieldOrMethod(buffer, true, bytesPkg, name)
		return obj.(*types.Func)
	}

	if view, ok := res.ViewOf(method("Bytes")); !ok || view.EndedBy != effect.Read|effect.Write|effect.Clear {
		t.Errorf("ViewOf(bytes.Buffer.Bytes) = %+v, %v; want the known view", view, ok)
	}

	reset := method("Reset")
	if res.EventsOn(reset, 0) != effect.Clear || res.EventsOn(reset, 1) != 0 || !res.EndsViews(reset) {
		t.Error("bytes.Buffer.Reset does not resolve to its known event on the receiver")
	}

	if res.EndsViews(method("Bytes")) {
		t.Error("bytes.Buffer.Bytes ends views; it only makes one")
	}
}

func TestFactString(t *testing.T) {
	for _, tc := range []struct {
		fact summary.Fact
		want string
	}{
		{summary.Fact{Invalidates: effect.ArgSet(0b101)}, "invalidates(0x5)"},
		{summary.Fact{View: effect.View{Arg: 1, EndedBy: effect.Write}}, "viewof(1: 0x2)"},
		{summary.Fact{Events: []effect.Event{effect.Clear, 0, effect.All}}, "ends(0: 0x4, 2: 0xf)"},
		{
			summary.Fact{Invalidates: 1, View: effect.View{EndedBy: effect.Read}, Events: []effect.Event{effect.Read}},
			"invalidates(0x1) viewof(0: 0x1) ends(0: 0x1)",
		},
	} {
		if got := tc.fact.String(); got != tc.want {
			t.Errorf("String() = %q, want %q", got, tc.want)
		}
	}
}
