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

package flow

import "golang.org/x/tools/go/ssa"

// DefaultBudget bounds the work for one function: instructions visited, summed over all
// iterations. It is far above what real functions need and stops pathological inputs.
const DefaultBudget = 2_000_000

// Problem defines one dataflow analysis. Facts are the integers 0..N-1.
type Problem interface {
	// N returns the number of facts.
	N() int
	// Phi updates state for a phi node. edge(i) is the out-state of the phi block's i-th
	// predecessor, or nil if that predecessor is unreachable.
	Phi(phi *ssa.Phi, edge func(i int) *Bitset, state *Bitset)
	// Transfer updates state for any other instruction.
	Transfer(instr ssa.Instruction, state *Bitset)
}

// Outcome reports whether a solution is usable.
type Outcome int

const (
	// Solved means the analysis reached its fixpoint.
	Solved Outcome = iota
	// Unknown means the budget ran out. The solution must not be used.
	Unknown
)

// Engine solves problems. Its buffers are reused across calls, so one Engine must not be
// used concurrently, and a Solution is valid only until the next call to Solve. An Engine must
// not be copied after its first Solve: it keeps a method value bound to itself, so a copy would
// read the original's buffers.
type Engine struct {
	// Budget is the instruction budget per function; zero means DefaultBudget.
	Budget int
	// BudgetHits counts the functions abandoned because of the budget.
	BudgetHits int

	in, out []Bitset
	reached []bool
	queued  []bool
	work    []int
	state   Bitset
	fn      *ssa.Function
	edgeFor *ssa.BasicBlock
	edge    func(int) *Bitset
}

// Solution holds the in-state of every reached block after Solve.
type Solution struct {
	eng *Engine
	p   Problem
}

// Solve runs p over fn, starting from the empty set at the entry block.
func (e *Engine) Solve(fn *ssa.Function, p Problem) (Solution, Outcome) {
	n := p.N()
	e.fn = fn
	e.grow(len(fn.Blocks))

	for i := range fn.Blocks {
		e.in[i].Reset(n)
		e.out[i].Reset(n)
		e.out[i].Fill() // top for a must analysis: not reached yet
		e.reached[i] = false
		e.queued[i] = false
	}

	e.state.Reset(n)

	if e.edge == nil {
		e.edge = e.edgeState
	}

	budget := e.Budget
	if budget == 0 {
		budget = DefaultBudget
	}

	e.work = e.work[:0]
	if len(fn.Blocks) > 0 {
		e.reached[0] = true
		e.push(0)
	}

	// The queue is consumed by index rather than by reslicing, so its storage is reused by the
	// next Solve.
	for head := 0; head < len(e.work); head++ { //nolint:intrange // the work list grows inside the loop
		i := e.work[head]
		e.queued[i] = false
		b := fn.Blocks[i]

		budget -= len(b.Instrs) + 1
		if budget < 0 {
			e.BudgetHits++
			return Solution{}, Unknown
		}

		e.meet(b)
		e.state.CopyFrom(&e.in[i])
		e.apply(b, p, nil)
		changed := !e.out[i].Equal(&e.state)
		e.out[i].CopyFrom(&e.state)

		for _, s := range b.Succs {
			if changed || !e.reached[s.Index] {
				e.reached[s.Index] = true
				e.push(s.Index)
			}
		}
	}

	return Solution{eng: e, p: p}, Solved
}

// Walk replays the solution in block order, calling visit with the state that holds
// immediately before each instruction. Unreached blocks are skipped.
func (s Solution) Walk(visit func(instr ssa.Instruction, state *Bitset)) {
	e := s.eng
	if e == nil {
		return
	}

	for _, b := range e.fn.Blocks {
		if !e.reached[b.Index] {
			continue
		}

		e.state.CopyFrom(&e.in[b.Index])
		e.apply(b, s.p, visit)
	}
}

func (e *Engine) grow(n int) {
	if cap(e.in) < n {
		e.in = make([]Bitset, n)
		e.out = make([]Bitset, n)
		e.reached = make([]bool, n)
		e.queued = make([]bool, n)
	}

	e.in, e.out = e.in[:n], e.out[:n]
	e.reached, e.queued = e.reached[:n], e.queued[:n]
}

func (e *Engine) push(i int) {
	if !e.queued[i] {
		e.queued[i] = true
		e.work = append(e.work, i)
	}
}

// meet sets in[b] to the intersection of the out-states of b's reached predecessors. The
// entry block keeps the empty set it was reset to.
func (e *Engine) meet(b *ssa.BasicBlock) {
	if b.Index == 0 {
		return
	}

	in := &e.in[b.Index]
	first := true

	for _, p := range b.Preds {
		if !e.reached[p.Index] {
			continue
		}

		if first {
			in.CopyFrom(&e.out[p.Index])

			first = false

			continue
		}

		in.IntersectWith(&e.out[p.Index])
	}
}

// apply runs b's instructions over e.state, calling visit (if non-nil) before each one.
func (e *Engine) apply(b *ssa.BasicBlock, p Problem, visit func(ssa.Instruction, *Bitset)) {
	e.edgeFor = b
	for _, instr := range b.Instrs {
		if visit != nil {
			visit(instr, &e.state)
		}

		if phi, ok := instr.(*ssa.Phi); ok {
			p.Phi(phi, e.edge, &e.state)
			continue
		}

		p.Transfer(instr, &e.state)
	}
}

// edgeState returns the out-state of the i-th predecessor of the block being applied, or nil
// if that predecessor is unreachable. It is a method value created once, so applying a block
// allocates nothing.
func (e *Engine) edgeState(i int) *Bitset {
	pred := e.edgeFor.Preds[i]
	if !e.reached[pred.Index] {
		return nil
	}

	return &e.out[pred.Index]
}
