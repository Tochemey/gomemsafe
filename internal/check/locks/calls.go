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

	"github.com/tochemey/gomemsafe/internal/directive"
	"github.com/tochemey/gomemsafe/internal/flow"
)

// refLock is a contract reference with the lock it names.
type refLock struct {
	ref directive.LockRef
	i   int
}

// call applies a call: of a function literal, which is solved in place; of a lock method; or
// of a function with a contract, whose preconditions are checked and whose effects applied.
func (p *locking) call(instr ssa.Instruction, f *frame, state *flow.Bitset) {
	call := instr.(ssa.CallInstruction)

	if child := p.u.inlineAt(instr, f); child != nil {
		p.inline(child, state)
		return
	}

	common := call.Common()
	pos := instr.Pos()

	if receiver, method, ok := lockMethod(common); ok {
		if i, ok := p.u.lockIndex[p.u.resolve(receiver, f, state, nil)]; ok {
			p.lockOp(i, method, pos, state)
		}

		return
	}

	callee, ok := common.Value.(*ssa.Function)
	if !ok {
		return
	}

	obj, ok := callee.Object().(*types.Func)
	if !ok {
		return
	}

	if contract, ok := p.c.sum.LockContractOf(obj); ok {
		p.contractCall(contract, obj, common.Args, call.Value(), pos, f, state)
	}
}

// lockOp applies a lock method to lock i. Locking a lock held on every path, or unlocking one
// not held on every path, or held in another mode, is reported.
func (p *locking) lockOp(i int, method string, pos token.Pos, state *flow.Bitset) {
	name := func() string { return p.c.name(p.u, p.u.locks[i]) }

	switch method {
	case methodLock, methodRLock, methodNestedLock:
		if p.reporting && p.silent == 0 && state.Has(p.u.held(i)) && !p.c.forced(pos) {
			p.c.reportLock(pos, name(), msgLocked)
		}

		p.lockBits(i, method != methodRLock, state)
	case methodUnlock, methodRUnlock, methodNestedUnlock:
		exclusive := method != methodRUnlock
		if p.reporting && p.silent == 0 && (!state.Has(p.u.held(i)) || state.Has(p.u.excl(i)) != exclusive) &&
			!p.c.forced(pos) {
			p.c.reportLock(pos, name(), msgUnlocked)
		}

		p.unlockBits(i, state)
	case methodDowngradeLock:
		state.Clear(p.u.excl(i))
		state.Set(p.u.notExcl(i))
	}
}

// contractCall checks the preconditions of a call of fn, whose contract is c, and applies its
// releases and acquires.
func (p *locking) contractCall(c *directive.LockContract, fn *types.Func, args []ssa.Value, result ssa.Value, pos token.Pos, f *frame, state *flow.Bitset) {
	report := p.reporting && p.silent == 0 && !p.c.forced(pos)

	for _, l := range p.refLocks(c.Excludes, args, result, f, state) {
		if !state.Has(p.u.free(l.i)) && report {
			p.c.reportCall(pos, fn, p.c.name(p.u, p.u.locks[l.i]), msgCallHolding, helpUnlockBefore)
		}
	}

	for _, l := range p.refLocks(c.Requires, args, result, f, state) {
		p.requireHeld(l, fn, msgCallWithout, pos, report, state)
	}

	released := p.refLocks(c.Releases, args, result, f, state)

	for _, l := range released {
		p.requireHeld(l, fn, msgCallReleasing, pos, report, state)
	}

	for _, l := range released {
		p.unlockBits(l.i, state)
	}

	for _, l := range p.refLocks(c.Acquires, args, result, f, state) {
		if state.Has(p.u.held(l.i)) && report {
			p.c.reportCall(pos, fn, p.c.name(p.u, p.u.locks[l.i]), msgCallAcquiring, helpUnlockBefore)
		}

		p.lockBits(l.i, l.ref.Exclusive, state)
	}
}

// requireHeld checks that the lock l names is held at a call of fn, in the mode the reference
// asks; a forced line locks it instead.
func (p *locking) requireHeld(l refLock, fn *types.Func, msg string, pos token.Pos, report bool, state *flow.Bitset) {
	need := p.u.held(l.i)
	if l.ref.Exclusive {
		need = p.u.excl(l.i)
	}

	switch {
	case state.Has(need):
	case p.c.forced(pos):
		p.lockBits(l.i, l.ref.Exclusive, state)
	case report:
		p.c.reportCall(pos, fn, p.c.name(p.u, p.u.locks[l.i]), msg, helpLockBefore)
	}
}

// refLocks returns the references of refs with the locks they name at a call with args and
// result, or, with no args, in the function of f; a reference that names no lock there is
// left out.
func (p *locking) refLocks(refs []directive.LockRef, args []ssa.Value, result ssa.Value, f *frame, state *flow.Bitset) []refLock {
	var out []refLock

	for _, ref := range refs {
		if i, ok := p.refIndex(ref, args, result, f, state); ok {
			out = append(out, refLock{ref, i})
		}
	}

	return out
}

// refIndex returns the lock the contract reference ref names at a call with args and result,
// or, with no args, in the function of f itself. A result lock of a call whose result is not
// extracted, or of a deferred call, names nothing.
func (p *locking) refIndex(ref directive.LockRef, args []ssa.Value, result ssa.Value, f *frame, state *flow.Bitset) (int, bool) {
	if ref.Kind == directive.LockGlobal {
		i, ok := p.u.lockIndex[key{global: ref.Global}.extend(ref.Path)]
		return i, ok
	}

	v, ok := refValue(ref, args, result, f)
	if !ok {
		return 0, false
	}

	i, ok := p.u.lockIndex[p.u.resolve(v, f, state, nil).extend(ref.Path)]

	return i, ok
}

// inline solves the literal of child from the state at its call, and continues with the state
// at its returns; a literal that never returns leaves the state as it was, as checklocks does.
// While reporting, the literal's solution is replayed too. The literal is solved at every
// visit of the call, on the frame's own engine, so that its buffers are reused.
func (p *locking) inline(child *frame, state *flow.Bitset) {
	child.entry.Reset(p.u.n)
	child.entry.CopyFrom(state)
	child.exit.Reset(p.u.n)
	child.exit.Fill()
	child.returned = false

	p.frames = append(p.frames, child)

	reporting := p.reporting
	p.reporting = false

	child.eng.Budget = p.budget

	sol, outcome := child.eng.Solve(child.fn, p)
	p.budget -= child.eng.Spent

	if outcome != flow.Solved || p.budget <= 0 {
		if !p.unknown {
			p.c.eng.BudgetHits++
		}

		p.unknown = true
	} else if reporting {
		p.reporting = true

		sol.Walk(func(ssa.Instruction, *flow.Bitset) {})
	}

	p.reporting = reporting
	p.frames = p.frames[:len(p.frames)-1]

	if !child.returned {
		return
	}

	state.CopyFrom(&child.exit)

	// As checklocks, the caller continues with the locks the literal holds on every path, in
	// one mode. A lock it holds on some paths only, or in different modes, is reported at the
	// literal, once, where checklocks reports incompatible return states.
	for i := range p.u.locks {
		if !p.inconsistent(i, state) {
			continue
		}

		if p.reporting && p.silent == 0 && !p.c.forced(child.fn.Pos()) {
			p.c.reportReturn(child.fn.Pos(), child.fn, p.c.name(p.u, p.u.locks[i]), msgReturnSomePaths, helpUnlockBeforeReturn)
		}

		p.unlockBits(i, state)
	}
}

// inconsistent reports whether lock i is held on some paths only, or in different modes on
// different paths, in state.
func (p *locking) inconsistent(i int, state *flow.Bitset) bool {
	held, free := state.Has(p.u.held(i)), state.Has(p.u.free(i))
	return !held && !free || held && !state.Has(p.u.excl(i)) && !state.Has(p.u.notExcl(i))
}

// runDefers applies the deferred calls of f's function in reverse order of registration: those
// pending on every path in full, those pending on some paths only for their releases alone,
// which assumes they pair with the acquire on those paths.
func (p *locking) runDefers(f *frame, state *flow.Bitset) {
	for j := len(p.u.defers) - 1; j >= 0; j-- {
		d := p.u.defers[j]
		if d.Parent() != f.fn {
			continue
		}

		switch {
		case state.Has(p.u.pending(j)):
			p.deferred(d, f, state)
		case !state.Has(p.u.unpended(j)):
			p.silent++
			p.deferred(d, f, state)
			p.silent--
		}

		state.Clear(p.u.pending(j))
		state.Set(p.u.unpended(j))
	}
}

// deferred applies the deferred call d as a call made now.
func (p *locking) deferred(d *ssa.Defer, f *frame, state *flow.Bitset) {
	p.call(d, f, state)
}
