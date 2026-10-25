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

package ownership

import (
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/effect"
	"github.com/tochemey/gomemsafe/internal/flow"
	"github.com/tochemey/gomemsafe/internal/invalidation"
)

// contracts answers which types are owned and which parameters consume their arguments, in this
// package and through facts in others: the summary analyzer's Result.
type contracts interface {
	Owned(t types.Type) bool
	Consumes(fn *types.Func) effect.ArgSet
}

// moves is the problem MS501 solves for one function. Fact c holds while class c still owns
// its value on every path: it is set where the value is made, or where a variable is assigned
// again, and cleared by a move. A value used where its fact does not hold may have been moved
// on some path, which is a use after move. Tracking ownership kept on every path, rather than
// moves made on some path, lets the must-engine solve a may-question: a value is maybe-moved
// exactly when it is not surely still owned.
type moves struct {
	fn      *ssa.Function
	bases   []ssa.Value
	classOf map[ssa.Value]int
	// variables maps each local variable whose content was moved to the class of that content.
	// The variable's own class, when it has one, is the pointer to it.
	variables map[*ssa.Alloc]int
	moves     []move
	// at maps an instruction to the moves it makes; edges maps a select's edge into a send case
	// to the moves that case makes; assigned maps a store into a whole variable to the class it
	// makes owned again.
	at       map[ssa.Instruction][]int
	edges    map[edge][]int
	assigned map[*ssa.Store]int
	// entry lists the classes of parameters and free variables, owned from the start.
	entry []int
}

// newMoves finds fn's moves and the classes of the values they move.
func newMoves(fn *ssa.Function, sum contracts) *moves {
	m := &moves{
		fn: fn, classOf: make(map[ssa.Value]int), variables: make(map[*ssa.Alloc]int),
		at: make(map[ssa.Instruction][]int), edges: make(map[edge][]int), assigned: make(map[*ssa.Store]int),
	}

	m.addOwnedValues(sum)

	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			m.addMoves(instr, sum)
		}
	}

	if len(m.moves) == 0 {
		return m
	}

	m.closeOverAliases()
	m.addAssignments()

	for _, v := range append(fnValues(fn.Params), fnValues(fn.FreeVars)...) {
		if c, ok := m.classOf[v]; ok && m.bases[c] == v {
			m.entry = append(m.entry, c)
		}
	}

	return m
}

// N implements flow.Problem.
func (m *moves) N() int { return len(m.bases) }

// Phi implements flow.Problem: a phi owns its value when every incoming value is owned on its
// edge. An incoming value that is not tracked, such as nil, owns nothing that could move.
func (m *moves) Phi(phi *ssa.Phi, edge func(int) *flow.Bitset, state *flow.Bitset) {
	c, ok := m.classOf[phi]
	if !ok || m.bases[c] != phi {
		return
	}

	for i, v := range phi.Edges {
		in := edge(i)
		if in == nil {
			continue
		}

		if vc, tracked := m.classOf[v]; tracked && !in.Has(vc) {
			state.Clear(c)
			return
		}
	}

	state.Set(c)
}

// Transfer implements flow.Problem. The first instruction makes the parameters and free
// variables owned; a move clears what it moves; a variable's declaration, and a store into the
// whole variable, make its content owned; and an instruction that makes a tracked value makes
// it owned. The order matters for a call that consumes an argument and returns a new value: the
// value is owned.
func (m *moves) Transfer(instr ssa.Instruction, state *flow.Bitset) {
	if instr == m.fn.Blocks[0].Instrs[0] {
		for _, c := range m.entry {
			state.Set(c)
		}
	}

	for _, i := range m.at[instr] {
		for _, c := range m.moves[i].classes {
			state.Clear(c)
		}
	}

	switch x := instr.(type) {
	case *ssa.Store:
		if c, assigned := m.assigned[x]; assigned {
			state.Set(c)
		}
	case *ssa.Alloc:
		if c, isVariable := m.variables[x]; isVariable {
			state.Set(c)
		}
	}

	if v, ok := instr.(ssa.Value); ok {
		if c, tracked := m.classOf[v]; tracked && m.bases[c] == v {
			state.Set(c)
		}
	}
}

// Edge implements flow.EdgeProblem: a select's send case moves its value on the edge into the
// case only.
func (m *moves) Edge(pred, succ *ssa.BasicBlock, state *flow.Bitset) {
	for _, i := range m.edges[edge{pred, succ}] {
		for _, c := range m.moves[i].classes {
			state.Clear(c)
		}
	}
}

// usedClasses returns the classes whose values instr uses through op: op's class, and, for a
// local variable whose content can move, that content, except where instr assigns the whole
// variable.
func (m *moves) usedClasses(instr ssa.Instruction, op ssa.Value) []int {
	var out []int

	if c, tracked := m.classOf[op]; tracked {
		out = append(out, c)
	}

	if alloc, ok := op.(*ssa.Alloc); ok {
		if c, isVariable := m.variables[alloc]; isVariable {
			if st, isStore := instr.(*ssa.Store); !isStore || st.Addr != op {
				out = append(out, c)
			}
		}
	}

	return out
}

// sourceClasses returns c followed by the classes that flow into it through phis, nearest
// first, mirroring package invalidation's.
func (m *moves) sourceClasses(c int) []int {
	order := []int{c}
	seen := map[int]bool{c: true}

	for i := 0; i < len(order); i++ {
		phi, ok := m.bases[order[i]].(*ssa.Phi)
		if !ok {
			continue
		}

		for _, v := range phi.Edges {
			if ec, tracked := m.classOf[v]; tracked && !seen[ec] {
				seen[ec] = true
				order = append(order, ec)
			}
		}
	}

	return order
}

// lastMove returns the move of class c, or of a class flowing into it, closest before pos, or
// the first such move when none precedes pos, such as one later in a loop. A class that is not
// owned at a use was cleared by a move, so there is one.
func (m *moves) lastMove(c int, pos token.Pos) move {
	sources := make(map[int]bool)
	for _, sc := range m.sourceClasses(c) {
		sources[sc] = true
	}

	best, first := -1, -1

	for i, mv := range m.moves {
		if !movesAny(mv, sources) {
			continue
		}

		if first < 0 {
			first = i
		}

		if p := mv.instr.Pos(); p < pos && (best < 0 || p > m.moves[best].instr.Pos()) {
			best = i
		}
	}

	if best < 0 {
		best = first
	}

	return m.moves[best]
}

// addOwnedValues gives each value of an owned type, or pointer to one, that is not another
// value under a new type its own class: the parameters, the free variables, and the values the
// function's instructions make. Globals and constants are not tracked: the function does not
// make them, so it cannot tell when they were owned.
func (m *moves) addOwnedValues(sum contracts) {
	for _, v := range append(fnValues(m.fn.Params), fnValues(m.fn.FreeVars)...) {
		if sum.Owned(v.Type()) {
			m.class(v)
		}
	}

	for _, b := range m.fn.Blocks {
		for _, instr := range b.Instrs {
			if v, ok := instr.(ssa.Value); ok && sum.Owned(v.Type()) && invalidation.Base(v) == v {
				m.class(v)
			}
		}
	}
}

// addAssignments records the stores into whole variables whose content can move: they make the
// content owned again.
func (m *moves) addAssignments() {
	for _, b := range m.fn.Blocks {
		for _, instr := range b.Instrs {
			if st, ok := instr.(*ssa.Store); ok {
				if alloc, isAlloc := st.Addr.(*ssa.Alloc); isAlloc {
					if c, isVariable := m.variables[alloc]; isVariable {
						m.assigned[st] = c
					}
				}
			}
		}
	}
}

// class returns the class whose base is v, creating it if needed.
func (m *moves) class(v ssa.Value) int {
	if c, ok := m.classOf[v]; ok {
		return c
	}

	c := m.newClass(v)
	m.classOf[v] = c

	return c
}

// newClass adds a class with base v: nil for a variable's content, which is no SSA value.
func (m *moves) newClass(v ssa.Value) int {
	m.bases = append(m.bases, v)
	return len(m.bases) - 1
}

// closeOverAliases adds to each class the values that are its value under another type, and
// gives each phi fed by a tracked value its own class, mirroring package invalidation's.
func (m *moves) closeOverAliases() {
	var work []ssa.Value

	for _, v := range m.bases {
		if v != nil {
			work = append(work, v)
		}
	}

	for len(work) > 0 {
		v := work[len(work)-1]
		work = work[:len(work)-1]

		for _, r := range referrersOf(v) {
			derived, ok := r.(ssa.Value)
			if !ok {
				continue
			}

			if _, seen := m.classOf[derived]; seen {
				continue
			}

			switch d := derived.(type) {
			case *ssa.Phi:
				m.class(d)
				work = append(work, d)
			case *ssa.MakeInterface, *ssa.ChangeType, *ssa.ChangeInterface:
				m.classOf[d] = m.classOf[v]
				work = append(work, d)
			case *ssa.TypeAssert:
				if !d.CommaOk {
					m.classOf[d] = m.classOf[v]
					work = append(work, d)
				}
			}
		}
	}
}

// isMade reports whether the function makes v: a parameter, a free variable, or the value of
// one of its instructions.
func isMade(v ssa.Value) bool {
	switch v.(type) {
	case *ssa.Parameter, *ssa.FreeVar:
		return true
	}

	_, ok := v.(ssa.Instruction)

	return ok
}

func movesAny(mv move, classes map[int]bool) bool {
	for _, c := range mv.classes {
		if classes[c] {
			return true
		}
	}

	return false
}

func fnValues[T ssa.Value](vs []T) []ssa.Value {
	out := make([]ssa.Value, len(vs))
	for i, v := range vs {
		out[i] = v
	}

	return out
}

func referrersOf(v ssa.Value) []ssa.Instruction {
	if refs := v.Referrers(); refs != nil {
		return *refs
	}

	return nil
}
