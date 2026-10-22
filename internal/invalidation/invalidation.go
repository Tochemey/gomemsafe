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

// Package invalidation is the analysis behind MS301 and behind the Invalidates summaries: it
// tracks, through one SSA function, values that a call has taken back, such as a buffer
// returned to a sync.Pool. A value is invalid at a point when an invalidating call ran on
// every path to that point and the value was not redefined since.
//
// Only values that share memory with the callee are tracked: pointers, slices, maps, channels,
// functions, unsafe pointers, and interfaces. Other values, such as an int, a string, or a
// struct, are copied by the call, so the caller's copy stays valid. Interfaces are tracked
// although their dynamic type is unknown, because the values held in a sync.Pool are pointers
// in practice (staticcheck's SA6002 flags the others).
//
// Values are grouped into alias classes: a value and everything that is the same memory under
// another type or length (interface conversions, type assertions, type changes, reslices).
// Invalidating any member invalidates the class. A phi node starts its own class and is invalid only if every incoming
// value was invalid on its edge. Values loaded from memory (fields, address-taken variables)
// are not tracked: Tier 1 may miss such bugs, but never reports a false one.
//
// Analyze builds the problem and solves it with package flow; Uses lists violations for
// checks, and InvalidAtReturn lists invalidated parameters for summaries.
package invalidation

import (
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/effect"
	"github.com/tochemey/gomemsafe/internal/flow"
)

// Lookup returns the call-argument indexes that a static callee invalidates.
type Lookup func(fn *types.Func) effect.ArgSet

// Site is one invalidating call.
type Site struct {
	Call   *ssa.Call
	Callee *types.Func
	// Args lists the invalidated call-argument indexes (receiver first for methods).
	Args []int
}

// Use is a read of a value after it was invalidated.
type Use struct {
	Instr ssa.Instruction
	Value ssa.Value
	// Site is the invalidating call the use comes after.
	Site Site
	// Arg is the invalidated call-argument index at Site.
	Arg int
}

// Analysis is the solved invalidation problem for one function.
type Analysis struct {
	Fn    *ssa.Function
	Sites []Site

	classOf map[ssa.Value]int
	bases   []ssa.Value
	// siteClasses[i] lists the classes Sites[i] invalidates.
	siteClasses [][]int
	siteOf      map[*ssa.Call]int
	solution    flow.Solution
}

// Analyze solves the problem for fn. It returns nil when fn contains no invalidating call or
// when the engine's budget runs out; either way there is nothing to report.
func Analyze(fn *ssa.Function, lookup Lookup, eng *flow.Engine) *Analysis {
	a := &Analysis{Fn: fn, classOf: make(map[ssa.Value]int), siteOf: make(map[*ssa.Call]int)}
	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			if call, ok := instr.(*ssa.Call); ok {
				a.addSite(call, lookup)
			}
		}
	}

	if len(a.Sites) == 0 {
		return nil
	}

	a.closeOverAliases()

	sol, outcome := eng.Solve(fn, a)
	if outcome != flow.Solved {
		return nil
	}

	a.solution = sol

	return a
}

// Base strips conversions and reslices that keep the same memory, returning the value they
// started from.
func Base(v ssa.Value) ssa.Value {
	for {
		switch x := v.(type) {
		case *ssa.MakeInterface:
			v = x.X
		case *ssa.Slice:
			v = x.X
		case *ssa.ChangeType:
			v = x.X
		case *ssa.ChangeInterface:
			v = x.X
		case *ssa.TypeAssert:
			if x.CommaOk {
				return v
			}

			v = x.X
		case *ssa.Extract:
			ta, ok := x.Tuple.(*ssa.TypeAssert)
			if !ok || x.Index != 0 {
				return v
			}

			v = ta.X
		default:
			return v
		}
	}
}

// N implements flow.Problem.
func (a *Analysis) N() int { return len(a.bases) }

// Phi implements flow.Problem: a phi class is invalid when every reachable incoming edge
// carries an invalid value.
func (a *Analysis) Phi(phi *ssa.Phi, edge func(int) *flow.Bitset, state *flow.Bitset) {
	c, ok := a.classOf[phi]
	if !ok || a.bases[c] != phi {
		return
	}

	invalid := false

	for i, v := range phi.Edges {
		in := edge(i)
		if in == nil {
			continue
		}

		vc, tracked := a.classOf[v]
		if !tracked || !in.Has(vc) {
			state.Clear(c)
			return
		}

		invalid = true
	}

	if invalid {
		state.Set(c)
		return
	}

	state.Clear(c)
}

// Transfer implements flow.Problem: redefining a class's base revalidates the class, and an
// invalidating call invalidates the classes of its arguments.
func (a *Analysis) Transfer(instr ssa.Instruction, state *flow.Bitset) {
	if v, ok := instr.(ssa.Value); ok {
		if c, tracked := a.classOf[v]; tracked && a.bases[c] == v {
			state.Clear(c)
		}
	}

	if call, ok := instr.(*ssa.Call); ok {
		if i, isSite := a.siteOf[call]; isSite {
			for _, c := range a.siteClasses[i] {
				state.Set(c)
			}
		}
	}
}

// Uses returns the first use of each invalidated class, in block order. Pointer-preserving
// conversions, phis, debug references, and nil comparisons are not uses.
func (a *Analysis) Uses() []Use {
	var out []Use

	reported := make(map[int]bool)

	a.solution.Walk(func(instr ssa.Instruction, state *flow.Bitset) {
		if !isUse(instr) {
			return
		}

		var ops [8]*ssa.Value
		for _, op := range instr.Operands(ops[:0]) {
			if op == nil || *op == nil {
				continue
			}

			c, tracked := a.classOf[*op]
			if !tracked || reported[c] || !state.Has(c) {
				continue
			}

			site, arg := a.siteFor(c, instr.Pos())
			reported[c] = true

			out = append(out, Use{Instr: instr, Value: *op, Site: site, Arg: arg})
		}
	})

	return out
}

// InvalidAtReturn reports, for each parameter of the function, whether it is invalid at every
// reachable return. It is false for all parameters when the function never returns.
func (a *Analysis) InvalidAtReturn() []bool {
	params := a.Fn.Params

	out := make([]bool, len(params))
	for i := range out {
		out[i] = true
	}

	returns := 0

	a.solution.Walk(func(instr ssa.Instruction, state *flow.Bitset) {
		if _, ok := instr.(*ssa.Return); !ok {
			return
		}

		returns++

		for i, p := range params {
			c, tracked := a.classOf[p]
			if !tracked || !state.Has(c) {
				out[i] = false
			}
		}
	})

	if returns == 0 {
		clear(out)
	}

	return out
}

// addSite records call as a site if its static callee invalidates any non-constant argument
// that shares memory with the caller.
func (a *Analysis) addSite(call *ssa.Call, lookup Lookup) {
	callee, ok := staticCallee(call)
	if !ok {
		return
	}

	bits := lookup(callee)
	if bits == 0 {
		return
	}

	site := Site{Call: call, Callee: callee}

	var classes []int

	for i, arg := range call.Common().Args {
		if !bits.Has(i) {
			continue
		}

		if _, isConst := arg.(*ssa.Const); isConst {
			continue
		}

		base := Base(arg)
		if !sharesMemory(base.Type()) {
			continue
		}

		site.Args = append(site.Args, i)
		classes = append(classes, a.class(base))
	}

	if len(site.Args) == 0 {
		return
	}

	a.siteOf[call] = len(a.Sites)
	a.Sites = append(a.Sites, site)
	a.siteClasses = append(a.siteClasses, classes)
}

// class returns the class whose base is v, creating it if needed.
func (a *Analysis) class(v ssa.Value) int {
	if c, ok := a.classOf[v]; ok {
		return c
	}

	c := len(a.bases)
	a.bases = append(a.bases, v)
	a.classOf[v] = c

	return c
}

// closeOverAliases adds to each class every value derived from its members by a
// pointer-preserving conversion, and gives each phi fed by a tracked value its own class.
func (a *Analysis) closeOverAliases() {
	work := append([]ssa.Value(nil), a.bases...)
	for len(work) > 0 {
		v := work[len(work)-1]
		work = work[:len(work)-1]
		c := a.classOf[v]

		for _, r := range referrersOf(v) {
			derived, ok := r.(ssa.Value)
			if !ok {
				continue
			}

			if _, seen := a.classOf[derived]; seen {
				continue
			}

			work = a.addDerived(derived, c, work)
		}
	}
}

// addDerived puts derived, a referrer of a member of class c, into the right class and returns
// the work list extended with the values whose referrers must be visited next.
func (a *Analysis) addDerived(derived ssa.Value, c int, work []ssa.Value) []ssa.Value {
	switch d := derived.(type) {
	case *ssa.Phi:
		a.class(d)
		return append(work, d)
	case *ssa.TypeAssert:
		a.classOf[d] = c
		if !d.CommaOk {
			return append(work, d)
		}

		for _, r := range referrersOf(d) {
			if ex, ok := r.(*ssa.Extract); ok && ex.Index == 0 {
				a.classOf[ex] = c
				work = append(work, ex)
			}
		}

		return work
	case *ssa.MakeInterface, *ssa.ChangeType, *ssa.ChangeInterface, *ssa.Slice:
		a.classOf[d] = c
		return append(work, d)
	}

	return work
}

// siteFor returns the invalidating site of class c that is closest before pos, or the first
// one when none precedes it (for example, a site earlier in a loop). A phi class has no site of
// its own; it takes the site of a class flowing into it. Every invalid class traces back to at
// least one site, because only sites invalidate.
func (a *Analysis) siteFor(c int, pos token.Pos) (Site, int) {
	for _, class := range a.sourceClasses(c) {
		if site, arg, ok := a.nearestSite(class, pos); ok {
			return site, arg
		}
	}

	return a.Sites[0], a.Sites[0].Args[0]
}

// sourceClasses returns c followed by the classes that flow into it through phis, nearest
// first.
func (a *Analysis) sourceClasses(c int) []int {
	order := []int{c}
	seen := map[int]bool{c: true}

	for i := 0; i < len(order); i++ {
		phi, ok := a.bases[order[i]].(*ssa.Phi)
		if !ok {
			continue
		}

		for _, v := range phi.Edges {
			if ec, tracked := a.classOf[v]; tracked && !seen[ec] {
				seen[ec] = true
				order = append(order, ec)
			}
		}
	}

	return order
}

// nearestSite returns the site invalidating class c that is closest before pos, or the first
// such site when none precedes pos.
func (a *Analysis) nearestSite(c int, pos token.Pos) (Site, int, bool) {
	best, bestArg := -1, -1
	first, firstArg := -1, -1

	for i, classes := range a.siteClasses {
		for j, sc := range classes {
			if sc != c {
				continue
			}

			if first < 0 {
				first, firstArg = i, a.Sites[i].Args[j]
			}

			p := a.Sites[i].Call.Pos()
			if p < pos && (best < 0 || p > a.Sites[best].Call.Pos()) {
				best, bestArg = i, a.Sites[i].Args[j]
			}
		}
	}

	if best < 0 {
		best, bestArg = first, firstArg
	}

	if best < 0 {
		return Site{}, 0, false
	}

	return a.Sites[best], bestArg, true
}

// staticCallee returns the function a call statically dispatches to, when the call's
// arguments line up with the function's parameters, receiver first. They do not for a method
// value: its SSA wrapper reports the method as its object but takes no receiver (see
// ssa.createBound), so such calls are skipped.
func staticCallee(call *ssa.Call) (*types.Func, bool) {
	fn := call.Common().StaticCallee()
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

// sharesMemory reports whether a value of type t refers to memory that a call can keep, so
// that the caller must not use it afterwards. Type parameters are not tracked: an
// instantiation may be a plain value.
func sharesMemory(t types.Type) bool {
	if _, isParam := t.(*types.TypeParam); isParam {
		return false
	}

	switch u := t.Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature, *types.Interface:
		return true
	case *types.Basic:
		return u.Kind() == types.UnsafePointer
	}

	return false
}

func referrersOf(v ssa.Value) []ssa.Instruction {
	if refs := v.Referrers(); refs != nil {
		return *refs
	}

	return nil
}

func isUse(instr ssa.Instruction) bool {
	switch x := instr.(type) {
	case *ssa.MakeInterface, *ssa.ChangeType, *ssa.ChangeInterface, *ssa.TypeAssert,
		*ssa.Extract, *ssa.Phi, *ssa.DebugRef:
		return false
	case *ssa.BinOp:
		return !isNilComparison(x)
	}

	return true
}

func isNilComparison(op *ssa.BinOp) bool {
	if op.Op != token.EQL && op.Op != token.NEQ {
		return false
	}

	isNil := func(v ssa.Value) bool {
		c, ok := v.(*ssa.Const)
		return ok && c.IsNil()
	}

	return isNil(op.X) || isNil(op.Y)
}
