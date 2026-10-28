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

package sharing

import (
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/flow"
)

// syncer decides which instructions may synchronise with another goroutine.
type syncer struct {
	// syncs reports whether a call of a function with an object may synchronise: the summary
	// analyzer's Result.Syncs.
	syncs func(*types.Func) bool
	// decided holds, for each function a call has reached, whether a call of it may synchronise,
	// so that the dataflow's iterations decide each callee once.
	decided map[*ssa.Function]bool
}

// synced is the dataflow problem of a goroutine's body. Its one fact holds where every path from
// the goroutine's start has synchronised.
type synced struct{ sync *syncer }

// N implements flow.Problem.
func (synced) N() int { return 1 }

// Phi implements flow.Problem: the engine meets the incoming states.
func (synced) Phi(*ssa.Phi, func(int) *flow.Bitset, *flow.Bitset) {}

// Transfer implements flow.Problem.
func (p synced) Transfer(instr ssa.Instruction, state *flow.Bitset) {
	if p.sync.synchronizes(instr) {
		state.Set(0)
	}
}

func newSyncer(syncs func(*types.Func) bool) *syncer {
	return &syncer{syncs: syncs, decided: make(map[*ssa.Function]bool)}
}

// synchronizes reports whether instr may synchronise with another goroutine: a channel receive
// or send, a select, or a call that may. A function's deferred calls run where it returns, so
// running them counts as a call of each.
func (s *syncer) synchronizes(instr ssa.Instruction) bool {
	switch x := instr.(type) {
	case *ssa.UnOp:
		return x.Op == token.ARROW
	case *ssa.Send, *ssa.Select, *ssa.RunDefers:
		return true
	case *ssa.Call:
		return s.calls(&x.Call)
	}

	return false
}

// unsynchronized returns, by free variable of the goroutine body fn, fn's accesses to it that
// some path from fn's start reaches without synchronising. It returns none when fn's dataflow
// exceeds eng's budget.
func (s *syncer) unsynchronized(fn *ssa.Function, eng *flow.Engine) [][]access {
	out := make([][]access, len(fn.FreeVars))
	found := false

	for i, fv := range fn.FreeVars {
		out[i] = accessesOf(fv)
		found = found || len(out[i]) > 0
	}

	if !found {
		return out
	}

	sol, outcome := eng.Solve(fn, synced{sync: s})
	if outcome != flow.Solved {
		return make([][]access, len(fn.FreeVars))
	}

	// An access in a block no path reaches is not reached unsynchronised either.
	unsynced := make(map[ssa.Instruction]bool)

	sol.Walk(func(instr ssa.Instruction, state *flow.Bitset) {
		if !state.Has(0) {
			unsynced[instr] = true
		}
	})

	for i := range out {
		out[i] = slices.DeleteFunc(out[i], func(a access) bool { return !unsynced[a.instr] })
	}

	return out
}

// calls reports whether call may synchronise. A call of an interface method or of a function
// value may run anything; a builtin never synchronises.
func (s *syncer) calls(call *ssa.CallCommon) bool {
	if call.IsInvoke() {
		return true
	}

	switch callee := call.Value.(type) {
	case *ssa.Builtin:
		return false
	case *ssa.Function:
		return s.function(callee)
	case *ssa.MakeClosure:
		return s.function(callee.Fn.(*ssa.Function))
	}

	return true
}

// function reports whether a call of fn may synchronise: from its summary, or, for a function
// without an object, such as a function literal, from its own instructions.
func (s *syncer) function(fn *ssa.Function) bool {
	if syncs, seen := s.decided[fn]; seen {
		return syncs
	}

	if obj, ok := fn.Object().(*types.Func); ok {
		s.decided[fn] = s.syncs(obj)
		return s.decided[fn]
	}

	// A function that calls itself does not synchronise through that call.
	s.decided[fn] = false

	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			if s.synchronizes(instr) {
				s.decided[fn] = true
				return true
			}
		}
	}

	return false
}
