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

	"github.com/tochemey/gomemsafe/internal/directive"
	"github.com/tochemey/gomemsafe/internal/summary"
)

func TestExportsLockFacts(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), summary.Analyzer, "locks")
}

// TestResultResolvesLockFacts resolves guards, locks, and contracts in the package that
// declares them and, through facts, in one that imports them, where the generic box is
// instantiated and a local field is guarded by an imported lock.
func TestResultResolvesLockFacts(t *testing.T) {
	results := analysistest.Run(t, analysistest.TestData(), summary.Analyzer, "locks", "locksclient")

	locks := results[0].Pass.Pkg
	counter := locks.Scope().Lookup("Counter").Type().Underlying().(*types.Struct)
	box := locks.Scope().Lookup("Box").Type().Underlying().(*types.Struct)
	mu, n, both := counter.Field(0), counter.Field(1), counter.Field(2)
	method := func(name string) *types.Func {
		obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(locks.Scope().Lookup("Counter").Type()), true, locks, name)
		return obj.(*types.Func)
	}

	for _, r := range results {
		res := r.Result.(*summary.Result)
		pkg := r.Pass.Pkg.Path()

		if got := res.GuardsOf(n); len(got) != 1 || got[0].Name != "mu" || got[0].Path[0] != 0 {
			t.Errorf("%s: GuardsOf(n) = %+v", pkg, got)
		}

		if got := res.GuardsOf(both); len(got) != 2 || got[1].Global != "locks.Mu" {
			t.Errorf("%s: GuardsOf(both) = %+v", pkg, got)
		}

		if got := res.GuardsOf(box.Field(1)); len(got) != 1 || got[0].Name != "mu" {
			t.Errorf("%s: GuardsOf(Box.value) = %+v", pkg, got)
		}

		if !res.IsLock(mu) || !res.IsLock(locks.Scope().Lookup("Mu")) || res.IsLock(n) || res.IsLock(counter.Field(3)) {
			t.Errorf("%s: IsLock is wrong for mu, Mu, n, or buf", pkg)
		}

		if !res.HasLockFacts(mu) || !res.HasLockFacts(n) || res.HasLockFacts(counter.Field(3)) {
			t.Errorf("%s: HasLockFacts is wrong for mu, n, or buf", pkg)
		}

		if c, ok := res.LockContractOf(method("Get")); !ok || len(c.Requires) != 1 || c.Requires[0].Exclusive {
			t.Errorf("%s: LockContractOf(Get) = %+v, %v", pkg, c, ok)
		}

		if c, ok := res.LockContractOf(method("Unchecked")); !ok || !c.Ignore || !res.HasLockFacts(method("Unchecked")) {
			t.Errorf("%s: LockContractOf(Unchecked) = %+v, %v", pkg, c, ok)
		}

		if c, ok := res.LockContractOf(method("Plain")); ok || res.HasLockFacts(method("Plain")) {
			t.Errorf("%s: LockContractOf(Plain) = %+v, %v; want none", pkg, c, ok)
		}

		// A second lookup of an imported object comes from the cache.
		first := res.IsLock(mu)
		if res.IsLock(mu) != first {
			t.Errorf("%s: IsLock(mu) changed between calls", pkg)
		}
	}

	client := results[1]
	res := client.Result.(*summary.Result)
	local := client.Pass.Pkg.Scope().Lookup("Counter").Type().Underlying().(*types.Struct)

	if got := res.GuardsOf(local.Field(1)); len(got) != 1 || got[0].Name != "Counter.mu" {
		t.Errorf("locksclient: GuardsOf(extra) = %+v", got)
	}

	if res.HasLockFacts(types.Universe.Lookup("true")) {
		t.Error("HasLockFacts(true) = true; a universe object has no package and no facts")
	}
}

func TestGuardFactString(t *testing.T) {
	for _, tc := range []struct {
		fact summary.GuardFact
		want string
	}{
		{summary.GuardFact{Lock: true}, "lock"},
		{summary.GuardFact{Guards: []directive.Guard{{Name: "mu"}}}, "guards(mu)"},
		{summary.GuardFact{Guards: []directive.Guard{{Name: "mu"}, {Name: "g.mu"}}, Lock: true}, "guards(mu, g.mu) lock"},
	} {
		if got := tc.fact.String(); got != tc.want {
			t.Errorf("String() = %q, want %q", got, tc.want)
		}
	}
}
