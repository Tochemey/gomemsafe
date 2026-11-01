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
	"testing"

	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/flow"
)

// TestLockBitsAndInconsistency drives the lock facts of one lock through the modes a lock
// state has: free, held exclusively, held for reading, held on some paths only, and held in
// different modes on different paths.
func TestLockBitsAndInconsistency(t *testing.T) {
	u := &universe{locks: []key{{global: "p.mu"}}}
	u.index()

	p := &locking{u: u}

	var state flow.Bitset

	state.Reset(u.n)
	state.Set(u.free(0))
	state.Set(u.notExcl(0))

	if p.inconsistent(0, &state) {
		t.Error("a free lock is inconsistent")
	}

	p.lockBits(0, true, &state)

	if !state.Has(u.held(0)) || !state.Has(u.excl(0)) || state.Has(u.free(0)) || state.Has(u.notExcl(0)) || p.inconsistent(0, &state) {
		t.Error("an exclusive lock has the wrong facts")
	}

	p.lockBits(0, false, &state)

	if !state.Has(u.held(0)) || state.Has(u.excl(0)) || !state.Has(u.notExcl(0)) || p.inconsistent(0, &state) {
		t.Error("a read lock has the wrong facts")
	}

	// Held on every path, exclusively on some: the modes differ.
	state.Clear(u.notExcl(0))

	if !p.inconsistent(0, &state) {
		t.Error("a lock held in different modes is consistent")
	}

	p.unlockBits(0, &state)

	if state.Has(u.held(0)) || !state.Has(u.free(0)) || !state.Has(u.notExcl(0)) || p.inconsistent(0, &state) {
		t.Error("an unlocked lock has the wrong facts")
	}

	// Held on some paths only: neither held nor free.
	state.Clear(u.free(0))

	if !p.inconsistent(0, &state) {
		t.Error("a lock held on some paths only is consistent")
	}
}

func TestReturnTupleIsAValue(t *testing.T) {
	pkg := buildSSA(t, identitySrc)
	ret := instrOf[*ssa.Return](t, pkg.Func("results"))
	tuple := resultsOf(ret)

	if tuple.Name() != "results" || tuple.String() != "results" || tuple.Pos() != ret.Pos() ||
		tuple.Parent() != ret.Parent() || tuple.Referrers() != nil {
		t.Error("the return tuple does not present the return")
	}

	if tuple.Type().String() != "(int, *p.T)" {
		t.Errorf("Type() = %s", tuple.Type())
	}

	if v, ok := resultOf(tuple, 1); !ok || v != ret.Results[1] {
		t.Errorf("resultOf(tuple, 1) = %v, %v; want the second result", v, ok)
	}

	none := resultsOf(instrOf[*ssa.Return](t, pkg.Func("use")))
	if _, ok := resultOf(none, 0); ok {
		t.Error("a return without results has a result")
	}
}
