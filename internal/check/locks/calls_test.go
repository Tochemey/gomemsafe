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

	"github.com/tochemey/gomemsafe/internal/flow"
)

// TestInlineOverBudgetAbandonsTheFunction gives the literals no budget: the first literal
// inlined is abandoned, which counts one budget hit for the function and marks it unknown, and
// the next ones add no hit. The frame stack is left as it was.
func TestInlineOverBudgetAbandonsTheFunction(t *testing.T) {
	pkg := buildSSA(t, literalsSrc)
	fn := pkg.Func("literals")
	c := &checker{sum: noFacts{}}
	u := newUniverse(fn, nil, c)
	p := &locking{c: c, u: u, frames: []*frame{u.root}, budget: 1}

	var state flow.Bitset

	state.Reset(u.n)

	for _, child := range u.inlined {
		p.inline(child, &state)
	}

	if !p.unknown || c.eng.BudgetHits != 1 || len(p.frames) != 1 {
		t.Fatalf("unknown %v, %d budget hits, %d frames; want true, 1, 1", p.unknown, c.eng.BudgetHits, len(p.frames))
	}
}

// TestLockOperationsChangeTheMode drives one lock through the lock methods: Lock holds it
// exclusively, DowngradeLock and RLock for reading, Unlock and RUnlock free it, and the nested
// forms act as the plain ones. Nothing is reported outside the replay, so no checker is needed.
func TestLockOperationsChangeTheMode(t *testing.T) {
	u := &universe{locks: []key{{global: "p.mu"}}}
	u.index()

	p := &locking{u: u}

	var state flow.Bitset

	state.Reset(u.n)
	state.Set(u.free(0))
	state.Set(u.notExcl(0))

	for _, step := range []struct {
		method              string
		held, excl, notExcl bool
	}{
		{methodLock, true, true, false},
		{methodDowngradeLock, true, false, true},
		{methodUnlock, false, false, true},
		{methodRLock, true, false, true},
		{methodRUnlock, false, false, true},
		{methodNestedLock, true, true, false},
		{methodNestedUnlock, false, false, true},
	} {
		p.lockOp(0, step.method, 0, &state)

		held, excl, notExcl := state.Has(u.held(0)), state.Has(u.excl(0)), state.Has(u.notExcl(0))
		if held != step.held || excl != step.excl || notExcl != step.notExcl || state.Has(u.free(0)) == held {
			t.Errorf("after %s: held %v, exclusive %v, not exclusive %v, free %v", step.method, held, excl, notExcl, state.Has(u.free(0)))
		}
	}
}
