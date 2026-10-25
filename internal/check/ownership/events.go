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
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/invalidation"
)

// moveKind is how a value was moved, for diagnostics.
type moveKind int

const (
	sentOnChannel moveKind = iota
	passedToConsumer
	storedIntoOwner
)

// move is one place where a value leaves the function's ownership.
type move struct {
	instr ssa.Instruction
	kind  moveKind
	// classes are the classes the move clears: the value's, and its variable's content when
	// the value was just loaded from a variable.
	classes []int
	// param is the consuming parameter, for a value passed to one.
	param *types.Var
}

// edge is a control-flow edge.
type edge struct{ from, to *ssa.BasicBlock }

// addMoves records the moves instr makes: a send of an owned value, a call or go statement
// passing a value to a consuming parameter, and a store of an owned value into another owner.
// A deferred call is no move: it consumes its argument when the function returns, after every
// use. A select's send cases move on the edge into their bodies (selectMoves).
func (m *moves) addMoves(instr ssa.Instruction, sum contracts) {
	switch x := instr.(type) {
	case *ssa.Send:
		if v := invalidation.Base(x.X); sum.Owned(v.Type()) {
			m.addMove(move{instr: instr, kind: sentOnChannel}, v)
		}
	case *ssa.Select:
		m.selectMoves(x, sum)
	case *ssa.Call, *ssa.Go:
		m.consumerMoves(instr.(ssa.CallInstruction), sum)
	case *ssa.Store:
		if v := invalidation.Base(x.Val); sum.Owned(v.Type()) && isOwner(x.Addr, v, sum) {
			m.addMove(move{instr: instr, kind: storedIntoOwner}, v)
		}
	case *ssa.MapUpdate:
		if v := invalidation.Base(x.Value); sum.Owned(v.Type()) && isOwner(x.Map, v, sum) {
			m.addMove(move{instr: instr, kind: storedIntoOwner}, v)
		}
	}
}

// consumerMoves records a move of each argument that call passes to a consuming parameter of
// its static callee.
func (m *moves) consumerMoves(call ssa.CallInstruction, sum contracts) {
	callee, ok := staticCallee(call.Common())
	if !ok {
		return
	}

	consumes := sum.Consumes(callee)
	if consumes == 0 {
		return
	}

	for i, arg := range call.Common().Args {
		if !consumes.Has(i) || isNil(arg) {
			continue
		}

		m.addMove(move{instr: call, kind: passedToConsumer, param: paramAt(callee, i)}, invalidation.Base(arg))
	}
}

// selectMoves records the moves of a select's send cases. Case i runs on the edge where the
// select's index equals i, the branch go/ssa builds after the select. When that branch is not
// found, the send moves at the select itself, which may report a use in another case: sound,
// at the price of a false finding.
func (m *moves) selectMoves(sel *ssa.Select, sum contracts) {
	for i, state := range sel.States {
		if state.Dir != types.SendOnly {
			continue
		}

		v := invalidation.Base(state.Send)
		if !sum.Owned(v.Type()) {
			continue
		}

		mv := move{instr: sel, kind: sentOnChannel}
		if e, ok := caseEdge(sel, i); ok {
			m.edges[e] = append(m.edges[e], len(m.moves))
			m.moves = append(m.moves, m.withClasses(mv, v))

			continue
		}

		m.addMove(mv, v)
	}
}

// addMove records mv at its instruction, moving v.
func (m *moves) addMove(mv move, v ssa.Value) {
	m.at[mv.instr] = append(m.at[mv.instr], len(m.moves))
	m.moves = append(m.moves, m.withClasses(mv, v))
}

// withClasses returns mv moving v: v's class, and the content of the variable v was loaded from,
// when nothing assigned the variable between the load and the move. A value the function does
// not make, such as a constant or a global, has no class: the function cannot tell when it was
// owned.
func (m *moves) withClasses(mv move, v ssa.Value) move {
	if isMade(v) {
		mv.classes = append(mv.classes, m.class(v))
	}

	if alloc, ok := loadedFrom(v, mv.instr); ok {
		c, seen := m.variables[alloc]
		if !seen {
			c = m.newClass(nil)
			m.variables[alloc] = c
		}

		mv.classes = append(mv.classes, c)
	}

	return mv
}

// isOwner reports whether addr, the address or map a store writes, is inside a value of an
// owned type other than v: a field, an element, or a map entry of it, or of what such a value
// holds, through any loads on the way. Every value on the way is checked, not only the first,
// because an owner can be held in a variable, which is loaded before its field is addressed.
func isOwner(addr, v ssa.Value, sum contracts) bool {
	for x := addr; ; {
		switch y := x.(type) {
		case *ssa.FieldAddr:
			x = y.X
		case *ssa.IndexAddr:
			x = y.X
		case *ssa.UnOp:
			if y.Op != token.MUL {
				return false
			}

			x = y.X
		default:
			return false
		}

		if x != v && sum.Owned(x.Type()) {
			return true
		}
	}
}

// caseEdge returns the edge into the body of send case i of sel: the true branch of the If
// that compares the select's index with i.
func caseEdge(sel *ssa.Select, i int) (edge, bool) {
	for _, r := range referrersOf(sel) {
		index, ok := r.(*ssa.Extract)
		if !ok || index.Index != 0 {
			continue
		}

		for _, r := range referrersOf(index) {
			cmp, ok := r.(*ssa.BinOp)
			if !ok || cmp.Op != token.EQL || !isConstInt(cmp.Y, i) {
				continue
			}

			for _, r := range referrersOf(cmp) {
				if branch, ok := r.(*ssa.If); ok {
					return edge{branch.Block(), branch.Block().Succs[0]}, true
				}
			}
		}
	}

	return edge{}, false
}

// loadedFrom returns the local variable v was loaded from just before the move at instr: in the
// same block, with no store into the whole variable between them, so that what moves is the
// variable's current content.
func loadedFrom(v ssa.Value, instr ssa.Instruction) (*ssa.Alloc, bool) {
	load, ok := v.(*ssa.UnOp)
	if !ok || load.Op != token.MUL {
		return nil, false
	}

	alloc, ok := load.X.(*ssa.Alloc)
	if !ok {
		return nil, false
	}

	// The scan stops at the move: loaded is set by the load, and cleared by a store into the
	// variable after it.
	loaded := false

	for _, i := range instr.Block().Instrs {
		if i == instr {
			break
		}

		if st, isStore := i.(*ssa.Store); isStore && st.Addr == alloc {
			loaded = false
		}

		if i == load {
			loaded = true
		}
	}

	return alloc, loaded
}

// staticCallee returns the function a call statically dispatches to, when the call's arguments
// line up with the function's parameters, receiver first: not for a method value, whose
// wrapper takes no receiver. It mirrors package invalidation's.
func staticCallee(call *ssa.CallCommon) (*types.Func, bool) {
	fn := call.StaticCallee()
	if fn == nil {
		return nil, false
	}

	obj, ok := fn.Object().(*types.Func)
	if !ok || arity(fn.Signature) != arity(obj.Signature()) {
		return nil, false
	}

	return obj, true
}

// arity counts a signature's parameters, the receiver included.
func arity(sig *types.Signature) int {
	n := sig.Params().Len()
	if sig.Recv() != nil {
		n++
	}

	return n
}

// paramAt returns the parameter of fn at call-argument index i, the receiver first.
func paramAt(fn *types.Func, i int) *types.Var {
	sig := fn.Signature()
	if sig.Recv() != nil {
		if i == 0 {
			return sig.Recv()
		}

		i--
	}

	return sig.Params().At(i)
}

func isNil(v ssa.Value) bool {
	c, ok := v.(*ssa.Const)
	return ok && c.IsNil()
}

func isConstInt(v ssa.Value, i int) bool {
	c, ok := v.(*ssa.Const)
	if !ok || c.Value == nil || c.Value.Kind() != constant.Int {
		return false
	}

	n, exact := constant.Int64Val(c.Value)

	return exact && n == int64(i)
}
