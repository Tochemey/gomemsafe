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

	"github.com/tochemey/gomemsafe/internal/flow"
)

// Names of the builtins MS503 looks at.
const (
	appendBuiltin = "append"
	lenBuiltin    = "len"
	capBuiltin    = "cap"
)

// overwrite is an append that may write into the backing array of slices other than its first
// argument: one whose first argument is a prefix of a longer slice.
type overwrite struct {
	call *ssa.Call
	// prefix is the reslice the append extends, such as a[:i].
	prefix *ssa.Slice
	// root identifies the group of slices that may share the backing array.
	root ssa.Value
	// classes are the classes of the group the append may overwrite: all but its first
	// argument's.
	classes []int
	// own lists the in-place filters the append belongs to, and other the filters on the
	// slices it may overwrite that it does not belong to (file filters.go).
	own, other []int
}

// aliasing is the problem MS503 solves for one function. Class c holds a slice that the
// function made or received, and fact c holds while no append may have written into its
// backing array since: it is set where the slice is made and cleared by an overwriting append
// of its group. A slice used where its fact does not hold may have changed under it on some
// path, as MS501 tracks a value that may have moved.
type aliasing struct {
	fn *ssa.Function
	// parent is the union-find forest of the slices that may share a backing array.
	parent  map[ssa.Value]ssa.Value
	classOf map[ssa.Value]int
	bases   []ssa.Value
	// n counts the facts: the classes, then two per in-place filter.
	n          int
	entry      []int
	overwrites []overwrite
	at         map[*ssa.Call]int
	// appendedOnly holds the reslices made only to extend appends: making one reads no element,
	// so it is no use.
	appendedOnly map[*ssa.Slice]bool
	filters      []filter
	// reads maps an in-place filter's read of the ranged slice to the filter, counters each
	// filter loop's index increment, entries each edge into a filter loop from outside it, and
	// filtersOf each class to the filters ranging over it.
	reads     map[ssa.Instruction]int
	counters  map[ssa.Value][]int
	entries   map[edge][]int
	filtersOf map[int][]int
}

// newAliasing finds fn's slices that checked says to follow, the slices sharing a backing
// array with them, and the appends that may overwrite them. It returns nil when no append may.
func newAliasing(fn *ssa.Function, checked func(ssa.Value) bool) *aliasing {
	a := &aliasing{
		fn: fn, parent: make(map[ssa.Value]ssa.Value), classOf: make(map[ssa.Value]int),
		at: make(map[*ssa.Call]int), appendedOnly: make(map[*ssa.Slice]bool),
		reads: make(map[ssa.Instruction]int), counters: make(map[ssa.Value][]int),
		entries: make(map[edge][]int), filtersOf: make(map[int][]int),
	}

	slices := a.collectSlices()

	roots := make(map[ssa.Value]bool)

	for _, v := range slices {
		if checked(v) {
			roots[a.find(v)] = true
		}
	}

	for _, v := range slices {
		if roots[a.find(v)] && isBase(v) {
			a.classOf[v] = len(a.bases)
			a.bases = append(a.bases, v)
		}
	}

	if len(a.bases) == 0 {
		return nil
	}

	for _, v := range slices {
		if ct, ok := v.(*ssa.ChangeType); ok {
			if c, tracked := a.classOf[ct.X]; tracked {
				a.classOf[ct] = c
			}
		}
	}

	for _, v := range slices {
		c, tracked := a.classOf[v]
		if !tracked {
			continue
		}

		for _, r := range referrersOf(v) {
			if mi, ok := r.(*ssa.MakeInterface); ok {
				a.classOf[mi] = c
			}
		}
	}

	a.addOverwrites()

	if len(a.overwrites) == 0 {
		return nil
	}

	a.n = len(a.bases)
	a.addFilters()

	for _, v := range append(fnValues(fn.Params), fnValues(fn.FreeVars)...) {
		if c, ok := a.classOf[v]; ok {
			a.entry = append(a.entry, c)
		}
	}

	return a
}

// N implements flow.Problem.
func (a *aliasing) N() int { return a.n }

// Phi implements flow.Problem: a phi's slice is intact when every incoming slice is intact on
// its edge. An incoming slice that is not followed, such as nil or a reslice, is taken as
// intact: the slice it reslices carries its own fact.
func (a *aliasing) Phi(phi *ssa.Phi, edge func(int) *flow.Bitset, state *flow.Bitset) {
	c, ok := a.classOf[phi]
	if !ok {
		return
	}

	intact := true

	for i, v := range phi.Edges {
		if in := edge(i); in != nil {
			if vc, tracked := a.classOf[v]; tracked && !in.Has(vc) {
				intact = false
			}
		}
	}

	a.setClass(c, intact, state)
}

// Transfer implements flow.Problem. The first instruction makes the parameters and free
// variables intact; an overwriting append clears its classes before its own result is made,
// and an instruction that makes a followed slice makes it intact.
func (a *aliasing) Transfer(instr ssa.Instruction, state *flow.Bitset) {
	if instr == a.fn.Blocks[0].Instrs[0] {
		for _, c := range a.entry {
			a.setClass(c, true, state)
		}
	}

	if call, ok := instr.(*ssa.Call); ok {
		if i, isOverwrite := a.at[call]; isOverwrite {
			a.clear(a.overwrites[i], state)
		}
	}

	v, ok := instr.(ssa.Value)
	if !ok {
		return
	}

	for _, f := range a.counters[v] {
		state.Set(a.filters[f].fresh)
	}

	if c, tracked := a.classOf[v]; tracked && a.bases[c] == v {
		a.setClass(c, true, state)
	}
}

// Edge implements flow.EdgeProblem: an in-place filter loop entered while the ranged slice may
// have been overwritten reads overwritten elements from its first iteration.
func (a *aliasing) Edge(pred, succ *ssa.BasicBlock, state *flow.Bitset) {
	for _, f := range a.entries[edge{pred, succ}] {
		if !state.Has(a.filters[f].class) {
			state.Clear(a.filters[f].unchanged)
		}
	}
}

// intact reports whether the slice of class c that instr uses is intact in state. An in-place
// filter's read of its ranged slice only needs the filter's own appends to be behind it.
func (a *aliasing) intact(instr ssa.Instruction, c int, state *flow.Bitset) bool {
	if state.Has(c) {
		return true
	}

	f, isRead := a.reads[instr]

	return isRead && a.filters[f].class == c && state.Has(a.filters[f].unchanged) && state.Has(a.filters[f].fresh)
}

// uses reports whether instr reads or writes the elements of the slices it takes: converting a
// slice to another slice type or to an interface, a phi, a debug reference, len, cap, a
// comparison with nil, and a reslice made only to extend appends touch no element. A reslice
// that feeds a phi is a use: the phi does not follow the slice it reslices.
func (a *aliasing) uses(instr ssa.Instruction) bool {
	switch x := instr.(type) {
	case *ssa.ChangeType, *ssa.MakeInterface, *ssa.Phi, *ssa.DebugRef:
		return false
	case *ssa.Slice:
		return !a.appendedOnly[x]
	case *ssa.Call:
		return !isBuiltin(x.Call.Value, lenBuiltin) && !isBuiltin(x.Call.Value, capBuiltin)
	case *ssa.BinOp:
		return (x.Op != token.EQL && x.Op != token.NEQ) || !isNil(x.X) && !isNil(x.Y)
	}

	return true
}

// lastOverwrite returns the overwrite of c's group closest before pos, or the first one when
// none precedes pos, such as one later in a loop.
func (a *aliasing) lastOverwrite(c int, pos token.Pos) overwrite {
	root := a.find(a.bases[c])
	best, first := -1, -1

	for i, ow := range a.overwrites {
		if ow.root != root {
			continue
		}

		if first < 0 {
			first = i
		}

		if p := ow.call.Pos(); p < pos && (best < 0 || p > a.overwrites[best].call.Pos()) {
			best = i
		}
	}

	if best < 0 {
		best = first
	}

	return a.overwrites[best]
}

// setClass sets or clears class c, and with it the facts of the filters ranging over it that
// say no other append ran since the slice was made.
func (a *aliasing) setClass(c int, intact bool, state *flow.Bitset) {
	if intact {
		state.Set(c)
	} else {
		state.Clear(c)
	}

	for _, f := range a.filtersOf[c] {
		if intact {
			state.Set(a.filters[f].unchanged)
		} else {
			state.Clear(a.filters[f].unchanged)
		}
	}
}

// clear applies an overwriting append: its classes may have been overwritten, its own filters
// have an append behind them this iteration, and the other filters on its group have seen an
// append that is not theirs.
func (a *aliasing) clear(ow overwrite, state *flow.Bitset) {
	for _, c := range ow.classes {
		state.Clear(c)
	}

	for _, f := range ow.own {
		state.Clear(a.filters[f].fresh)
	}

	for _, f := range ow.other {
		state.Clear(a.filters[f].unchanged)
		state.Clear(a.filters[f].fresh)
	}
}

// collectSlices returns the parameters, free variables, and instruction values of fn whose
// type is a slice, and joins the ones that may share a backing array: a reslice of a slice and
// the slice, an append's result and its first argument unless that is capped, a phi and its
// incoming slices, and a conversion and its operand.
func (a *aliasing) collectSlices() []ssa.Value {
	var out []ssa.Value

	for _, v := range append(fnValues(a.fn.Params), fnValues(a.fn.FreeVars)...) {
		if isSlice(v.Type()) {
			out = append(out, v)
		}
	}

	for _, b := range a.fn.Blocks {
		for _, instr := range b.Instrs {
			v, ok := instr.(ssa.Value)
			if !ok || !isSlice(v.Type()) {
				continue
			}

			out = append(out, v)

			switch x := v.(type) {
			case *ssa.Slice:
				a.joinSlice(x, x.X)
			case *ssa.ChangeType:
				a.joinSlice(x, x.X)
			case *ssa.Phi:
				for _, e := range x.Edges {
					a.joinSlice(x, e)
				}
			case *ssa.Call:
				if isBuiltin(x.Call.Value, appendBuiltin) && !isCapped(x.Call.Args[0]) {
					a.joinSlice(x, x.Call.Args[0])
				}
			}
		}
	}

	return out
}

// joinSlice joins the groups of v and w when w is a slice the function holds, not a constant.
func (a *aliasing) joinSlice(v, w ssa.Value) {
	if _, isConst := w.(*ssa.Const); isConst || !isSlice(w.Type()) {
		return
	}

	if rv, rw := a.find(v), a.find(w); rv != rw {
		a.parent[rv] = rw
	}
}

// find returns the root of v's group.
func (a *aliasing) find(v ssa.Value) ssa.Value {
	for {
		p, ok := a.parent[v]
		if !ok {
			return v
		}

		if gp, grand := a.parent[p]; grand {
			a.parent[v] = gp
		}

		v = p
	}
}

// addOverwrites records the appends whose first argument extends a prefix of a followed slice,
// and the reslices made only to extend them.
func (a *aliasing) addOverwrites() {
	members := make(map[ssa.Value][]int)
	for c, base := range a.bases {
		members[a.find(base)] = append(members[a.find(base)], c)
	}

	for _, b := range a.fn.Blocks {
		for _, instr := range b.Instrs {
			call, ok := instr.(*ssa.Call)
			if !ok || !isBuiltin(call.Call.Value, appendBuiltin) {
				continue
			}

			first := call.Call.Args[0]

			prefix := prefixOf(first)
			if prefix == nil {
				continue
			}

			root := a.find(prefix.X)
			own, hasOwn := a.classOf[first]

			var classes []int

			for _, c := range members[root] {
				if !hasOwn || c != own {
					classes = append(classes, c)
				}
			}

			if len(classes) == 0 {
				continue
			}

			a.at[call] = len(a.overwrites)
			a.overwrites = append(a.overwrites, overwrite{call: call, prefix: prefix, root: root, classes: classes})
		}
	}

	for _, b := range a.fn.Blocks {
		for _, instr := range b.Instrs {
			if s, ok := instr.(*ssa.Slice); ok && a.onlyExtends(s) {
				a.appendedOnly[s] = true
			}
		}
	}
}

// onlyExtends reports whether every use of s is the first argument of an overwriting append.
func (a *aliasing) onlyExtends(s *ssa.Slice) bool {
	for _, r := range referrersOf(s) {
		call, isCall := r.(*ssa.Call)
		if _, isOverwrite := a.at[call]; !isCall || !isOverwrite || call.Call.Args[0] != s {
			return false
		}
	}

	return true
}

// prefixOf returns the reslice whose prefix v extends, following v through phis, the first
// arguments of appends, conversions, and reslices that keep the end of their slice: a reslice
// x[lo:hi] whose high bound is given and is not len(x). A capped reslice, x[lo:hi:hi], gives
// none: append copies it.
func prefixOf(v ssa.Value) *ssa.Slice {
	seen := make(map[ssa.Value]bool)
	work := []ssa.Value{v}

	for len(work) > 0 {
		v := work[len(work)-1]
		work = work[:len(work)-1]

		if seen[v] {
			continue
		}

		seen[v] = true

		switch x := v.(type) {
		case *ssa.Phi:
			work = append(work, x.Edges...)
		case *ssa.ChangeType:
			work = append(work, x.X)
		case *ssa.Call:
			if isBuiltin(x.Call.Value, appendBuiltin) && !isCapped(x.Call.Args[0]) {
				work = append(work, x.Call.Args[0])
			}
		case *ssa.Slice:
			switch {
			case !isSlice(x.X.Type()) || isCapped(x):
			case x.High == nil || isLenOf(x.High, x.X):
				work = append(work, x.X)
			default:
				return x
			}
		}
	}

	return nil
}

// isBase reports whether v is a slice MS503 gives a class: any but a constant, a conversion,
// which shares its operand's class, and a reslice of a slice, which is followed through the
// slice it reslices.
func isBase(v ssa.Value) bool {
	switch x := v.(type) {
	case *ssa.Const, *ssa.ChangeType:
		return false
	case *ssa.Slice:
		return !isSlice(x.X.Type())
	}

	return true
}

// isCapped reports whether v is a full reslice x[lo:hi:max] whose capacity ends where its
// length does, so that an append to it copies it.
func isCapped(v ssa.Value) bool {
	s, ok := v.(*ssa.Slice)
	return ok && s.Max != nil && sameIndex(s.High, s.Max)
}

// sameIndex reports whether two slice bounds are the same value, equal constants, or the same
// arithmetic on such, as the two i+1 of a[:i+1:i+1], which go/ssa computes twice.
func sameIndex(x, y ssa.Value) bool {
	if x == y {
		return true
	}

	switch x := x.(type) {
	case *ssa.Const:
		cy, ok := y.(*ssa.Const)
		return ok && x.Value != nil && cy.Value != nil && constant.Compare(x.Value, token.EQL, cy.Value)
	case *ssa.BinOp:
		by, ok := y.(*ssa.BinOp)
		return ok && x.Op == by.Op && sameIndex(x.X, by.X) && sameIndex(x.Y, by.Y)
	}

	return false
}

// isLenOf reports whether bound is len(x).
func isLenOf(bound, x ssa.Value) bool {
	call, ok := bound.(*ssa.Call)
	return ok && isBuiltin(call.Call.Value, lenBuiltin) && call.Call.Args[0] == x
}

// isBuiltin reports whether v is the builtin function name.
func isBuiltin(v ssa.Value, name string) bool {
	b, ok := v.(*ssa.Builtin)
	return ok && b.Name() == name
}

// isSlice reports whether t is a slice type.
func isSlice(t types.Type) bool {
	_, ok := t.Underlying().(*types.Slice)
	return ok
}
