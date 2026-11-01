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
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/flow"
)

// returnTuple presents a return's results as one value, so that resultOf finds each by index.
type returnTuple struct{ ret *ssa.Return }

// locking is the dataflow problem of one function. Per lock it holds whether every path holds
// it, holds it exclusively, and leaves it free; per deferred call whether every path has
// registered it, and whether none has; per tracked local variable and value whether the
// variable holds that value; and per load of such a variable which value it yielded. A
// function literal called or deferred directly is solved in place, from the state at the call,
// and the state at its returns continues after the call.
type locking struct {
	c *checker
	u *universe
	// frames is the stack of functions being solved; the top is the one whose instruction
	// Transfer sees.
	frames []*frame
	// reporting is set while the solution is replayed: only then are diagnostics emitted.
	reporting bool
	// silent counts the deferred calls applied that are pending on some paths only: their
	// releases are applied, and nothing is reported or checked.
	silent int
	// budget is what remains of the function's dataflow budget for the literals solved in
	// place, so that a function and its literals together stay within one budget.
	budget int
	// unknown is set when the literals exceeded the budget: nothing is reported.
	unknown bool
	// exit meets the states at the outermost function's returns, to report a function whose
	// returns hold different locks; returned says one was seen.
	exit     flow.Bitset
	returned bool
}

// N implements flow.Problem.
func (p *locking) N() int { return p.u.n }

// Phi implements flow.Problem: the engine meets the incoming states.
func (p *locking) Phi(*ssa.Phi, func(int) *flow.Bitset, *flow.Bitset) {}

// Edge implements flow.EdgeProblem. On a back edge the locks become free and the defers not
// pending: the state a later iteration of a loop produces is not followed after the loop,
// as checklocks analyses a loop body once, with the state it is first entered with. The must
// facts stay sound, since a lock held on every path through the loop is held after it.
func (p *locking) Edge(pred, succ *ssa.BasicBlock, state *flow.Bitset) {
	if !succ.Dominates(pred) {
		return
	}

	for i := range p.u.locks {
		state.Set(p.u.free(i))
		state.Set(p.u.notExcl(i))
	}

	for j := range p.u.defers {
		state.Set(p.u.unpended(j))
	}
}

// Transfer implements flow.Problem.
func (p *locking) Transfer(instr ssa.Instruction, state *flow.Bitset) {
	f := p.frames[len(p.frames)-1]
	if instr == f.fn.Blocks[0].Instrs[0] {
		p.enter(f, state)
	}

	p.globalAccesses(instr, f, state)

	switch x := instr.(type) {
	case *ssa.Store:
		p.store(x, f, state)
	case *ssa.UnOp:
		if x.Op == token.MUL {
			p.load(x, f, state)
		}
	case *ssa.FieldAddr:
		p.fieldAccess(x, x.X, x.Field, isWrite(x), f, state)
	case *ssa.Field:
		p.fieldAccess(x, x.X, x.Field, false, f, state)
	case *ssa.Call:
		p.call(x, f, state)
	case *ssa.Defer:
		if j, ok := p.u.deferIndex[x]; ok {
			state.Set(p.u.pending(j))
			state.Clear(p.u.unpended(j))
		}
	case *ssa.RunDefers:
		p.runDefers(f, state)
	case *ssa.Return:
		p.returns(x, f, state)
	}
}

// enter sets the state at f's first instruction: for an inlined literal the caller's state at
// the call; otherwise no lock held and no defer pending, then the locks the function's
// contract says its callers hold.
func (p *locking) enter(f *frame, state *flow.Bitset) {
	if f.parent != nil {
		state.CopyFrom(&f.entry)
		return
	}

	for i := range p.u.locks {
		state.Set(p.u.free(i))
		state.Set(p.u.notExcl(i))
	}

	for j := range p.u.defers {
		state.Set(p.u.unpended(j))
	}

	if f.contract == nil {
		return
	}

	for _, ref := range f.contract.HeldOnEntry() {
		if i, ok := p.refIndex(ref, nil, nil, f, state); ok {
			p.lockBits(i, ref.Exclusive, state)
		}
	}
}

// store records the value a tracked local variable now holds.
func (p *locking) store(s *ssa.Store, f *frame, state *flow.Bitset) {
	alloc, ok := p.u.isAlloc(p.u.resolve(s.Addr, f, state, nil))
	if !ok {
		return
	}

	for _, stored := range p.u.stores[alloc] {
		state.Clear(p.u.holds[holdPair{alloc, stored}])
	}

	if i, ok := p.u.holds[holdPair{alloc, stored{s.Val, f}}]; ok {
		state.Set(i)
	}
}

// load records which value a load of a tracked local variable yields: the one the variable
// holds on every path here.
func (p *locking) load(load *ssa.UnOp, f *frame, state *flow.Bitset) {
	alloc, ok := p.u.isAlloc(p.u.resolve(load.X, f, state, nil))
	if !ok {
		return
	}

	for _, stored := range p.u.stores[alloc] {
		i, ok := p.u.is[isPair{load, stored}]
		if !ok {
			continue
		}

		if state.Has(p.u.holds[holdPair{alloc, stored}]) {
			state.Set(i)
		} else {
			state.Clear(i)
		}
	}
}

// globalAccesses checks the guards of every global instr accesses, written when instr stores
// to it.
func (p *locking) globalAccesses(instr ssa.Instruction, f *frame, state *flow.Bitset) {
	var ops [8]*ssa.Value

	for _, op := range instr.Operands(ops[:0]) {
		g, ok := (*op).(*ssa.Global)
		if !ok {
			continue
		}

		s, isStore := instr.(*ssa.Store)
		write := isStore && s.Addr == g

		p.access(instr, g.Object(), key{}, write, f, state)
	}
}

// fieldAccess checks the guards of the field of base that instr accesses. A field of a value
// the function allocated itself is not checked, as checklocks does not check it.
func (p *locking) fieldAccess(instr ssa.Instruction, base ssa.Value, field int, write bool, f *frame, state *flow.Bitset) {
	obj, ok := fieldOf(base.Type(), field)
	if !ok || fresh(base) {
		return
	}

	p.access(instr, obj, p.u.resolve(base, f, state, nil), write, f, state)
}

// access checks that every guard of obj is held, exclusively for a write, at instr. A forced
// line locks the guard instead.
func (p *locking) access(instr ssa.Instruction, obj types.Object, base key, write bool, f *frame, state *flow.Bitset) {
	if p.silent > 0 {
		return
	}

	guards := p.c.sum.GuardsOf(obj)
	if len(guards) == 0 {
		return
	}

	pos := p.c.accessPos(instr, obj, f)

	for _, guard := range guards {
		i, ok := p.u.lockIndex[guardKey(guard, base)]
		if !ok {
			continue
		}

		need := p.u.held(i)
		if write {
			need = p.u.excl(i)
		}

		switch {
		case state.Has(need):
		case p.c.forced(pos):
			p.lockBits(i, true, state)
		case p.reporting:
			p.c.reportAccess(pos, obj, p.c.name(p.u, p.u.locks[i]), write)
		}
	}
}

// returns checks the locks at a return of the outermost function: the contract's locks on exit
// must be held, and no other lock may be. In an inlined literal the state is collected for the
// caller instead. An implicit return is reported at the function.
func (p *locking) returns(ret *ssa.Return, f *frame, state *flow.Bitset) {
	if f.parent != nil {
		if !f.returned {
			f.exit.CopyFrom(state)
			f.returned = true
		} else {
			f.exit.IntersectWith(state)
		}

		return
	}

	if !p.reporting || p.silent > 0 {
		return
	}

	pos := ret.Pos()
	if !pos.IsValid() {
		pos = f.fn.Pos()
	}

	expected := make(map[int]bool)

	if f.contract != nil {
		for _, ref := range f.contract.HeldOnExit() {
			i, ok := p.refIndex(ref, nil, resultsOf(ret), f, state)
			if !ok {
				continue
			}

			expected[i] = true

			need := p.u.held(i)
			if ref.Exclusive {
				need = p.u.excl(i)
			}

			switch {
			case state.Has(need):
			case p.c.forced(pos):
				p.lockBits(i, ref.Exclusive, state)
			default:
				p.c.reportReturn(pos, f.fn, ref.Name, msgReturnWithout, helpLockBeforeReturn)
			}
		}
	}

	if !p.returned {
		p.exit.Reset(p.u.n)
		p.exit.CopyFrom(state)
		p.returned = true
	} else {
		p.exit.IntersectWith(state)
	}

	if p.c.forced(pos) {
		return
	}

	for i := range p.u.locks {
		if !state.Has(p.u.free(i)) && !expected[i] {
			p.c.reportReturn(pos, f.fn, p.c.name(p.u, p.u.locks[i]), msgReturnHolding, helpUnlockBeforeReturn)
		}
	}
}

// inconsistentReturns reports the outermost function when its returns do not all hold the same
// locks in the same modes, as checklocks reports incompatible return states: a lock held at
// some return, or on some path to one, and not at another, or in different modes.
func (p *locking) inconsistentReturns() {
	if !p.returned || p.c.forced(p.u.root.fn.Pos()) {
		return
	}

	for i := range p.u.locks {
		if p.inconsistent(i, &p.exit) {
			p.c.reportReturn(p.u.root.fn.Pos(), p.u.root.fn, p.c.name(p.u, p.u.locks[i]), msgReturnSomePaths, helpUnlockBeforeReturn)
		}
	}
}

// lockBits marks lock i held, in the given mode.
func (p *locking) lockBits(i int, exclusive bool, state *flow.Bitset) {
	state.Set(p.u.held(i))
	state.Clear(p.u.free(i))

	if exclusive {
		state.Set(p.u.excl(i))
		state.Clear(p.u.notExcl(i))
	} else {
		state.Clear(p.u.excl(i))
		state.Set(p.u.notExcl(i))
	}
}

// unlockBits marks lock i free.
func (p *locking) unlockBits(i int, state *flow.Bitset) {
	state.Clear(p.u.held(i))
	state.Clear(p.u.excl(i))
	state.Set(p.u.free(i))
	state.Set(p.u.notExcl(i))
}

// resultsOf returns the value a return's results form, for resolving a contract's result
// locks: the single result, or the tuple.
func resultsOf(ret *ssa.Return) ssa.Value {
	if len(ret.Results) == 1 {
		return ret.Results[0]
	}

	return returnTuple{ret}
}

func (t returnTuple) Name() string                  { return "results" }
func (t returnTuple) String() string                { return "results" }
func (t returnTuple) Type() types.Type              { return t.ret.Parent().Signature.Results() }
func (t returnTuple) Parent() *ssa.Function         { return t.ret.Parent() }
func (t returnTuple) Referrers() *[]ssa.Instruction { return nil }
func (t returnTuple) Pos() token.Pos                { return t.ret.Pos() }
