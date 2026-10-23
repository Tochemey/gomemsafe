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
// Values of a type parameter are tracked too, so that a generic wrapper such as zap's
// pool.Pool[T].Put gets a summary; each call site then decides from its concrete argument
// whether memory is shared. Their uses inside the generic body are not reported, because an
// instantiation may be a plain value.
//
// Values are grouped into alias classes (file classes.go): a value and everything that is the
// same memory under another type or length (interface conversions, type assertions, type
// changes, reslices).
// Invalidating any member invalidates the class. A phi node starts its own class and is invalid only if every incoming
// value was invalid on its edge. Values loaded from memory (fields, address-taken variables)
// are not tracked: Tier 1 may miss such bugs, but never reports a false one.
//
// AnalyzeWithViews also tracks views (file views.go): results that point into a receiver's
// buffer, made invalid by a later call on that receiver.
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
	// View is the view the used value came from when Site ended that view (MS302). It is nil
	// for a use after an invalidation (MS301).
	View *View
}

// Analysis is the solved invalidation problem for one function.
type Analysis struct {
	Fn    *ssa.Function
	Sites []Site

	classOf map[ssa.Value]int
	bases   []ssa.Value
	// siteClasses[i] lists the classes Sites[i] invalidates, and siteArgs[i] the call argument
	// that holds each of them. endsViews[i] is set when Sites[i] ends views rather than taking
	// its arguments back.
	siteClasses [][]int
	siteArgs    [][]int
	endsViews   []bool
	// siteOf lists the sites of each call: a call can both invalidate and end views.
	siteOf map[*ssa.Call][]int
	views  []View
	// viewClasses maps each class that is a view to what it views: the classes of view calls,
	// and phis of them.
	viewClasses map[int]viewClass
	// fieldOwners maps each field held by value to the first address of it, the owner that
	// stands for every other address of the same field (ownerOf).
	fieldOwners map[fieldKey]ssa.Value
	solution    flow.Solution
}

// Analyze solves the problem for fn. It returns nil when fn contains no invalidating call or
// when the engine's budget runs out; either way there is nothing to report.
func Analyze(fn *ssa.Function, lookup Lookup, eng *flow.Engine) *Analysis {
	return analyze(fn, lookup, eng, false)
}

// AnalyzeWithViews is Analyze that also tracks views: see View.
func AnalyzeWithViews(fn *ssa.Function, lookup Lookup, eng *flow.Engine) *Analysis {
	return analyze(fn, lookup, eng, true)
}

func analyze(fn *ssa.Function, lookup Lookup, eng *flow.Engine, withViews bool) *Analysis {
	a := &Analysis{
		Fn:      fn,
		classOf: make(map[ssa.Value]int),
		siteOf:  make(map[*ssa.Call][]int),
	}

	var calls []*ssa.Call

	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			if call, ok := instr.(*ssa.Call); ok {
				calls = append(calls, call)
				a.addSite(call, lookup)
			}
		}
	}

	if withViews {
		a.addViews(calls)
	}

	if len(a.Sites) == 0 && len(a.views) == 0 {
		return nil
	}

	a.closeOverAliases()

	if withViews {
		a.endViews(calls)
	}

	if len(a.Sites) == 0 {
		return nil
	}

	sol, outcome := eng.Solve(fn, a)
	if outcome != flow.Solved {
		return nil
	}

	a.solution = sol

	return a
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

// Transfer implements flow.Problem: an invalidating call invalidates the classes of its
// arguments, then redefining a class's base revalidates the class. The order matters for a call
// that ends its receiver's views and returns a new one, such as a second bytes.Buffer.Next: the
// new view is valid.
func (a *Analysis) Transfer(instr ssa.Instruction, state *flow.Bitset) {
	if call, ok := instr.(*ssa.Call); ok {
		for _, i := range a.siteOf[call] {
			for _, c := range a.siteClasses[i] {
				state.Set(c)
			}
		}
	}

	if v, ok := instr.(ssa.Value); ok {
		if c, tracked := a.classOf[v]; tracked && a.bases[c] == v {
			state.Clear(c)
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
			if !tracked || reported[c] || !state.Has(c) || isTypeParam(a.bases[c].Type()) {
				continue
			}

			site, arg, source := a.siteFor(c, instr.Pos())

			var view *View

			if a.endsViews[site] {
				if readsOnlyLength(instr) {
					continue
				}

				// A site that ends views lists only classes that hold one.
				view = &a.views[a.viewClasses[source].view]
			}

			reported[c] = true

			out = append(out, Use{Instr: instr, Value: *op, Site: a.Sites[site], Arg: arg, View: view})
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
		if !sharesMemory(base.Type()) && !isTypeParam(base.Type()) {
			continue
		}

		site.Args = append(site.Args, i)
		classes = append(classes, a.class(base))
	}

	if len(site.Args) == 0 {
		return
	}

	a.addSiteClasses(site, classes, site.Args, false)
}

// addSiteClasses records site as invalidating classes; args[i] is the call argument that holds
// classes[i]. endsViews tells a site that ends views from one that takes its arguments back.
func (a *Analysis) addSiteClasses(site Site, classes, args []int, endsViews bool) {
	a.siteOf[site.Call] = append(a.siteOf[site.Call], len(a.Sites))
	a.Sites = append(a.Sites, site)
	a.siteClasses = append(a.siteClasses, classes)
	a.siteArgs = append(a.siteArgs, args)
	a.endsViews = append(a.endsViews, endsViews)
}

// siteFor returns the index of the invalidating site of class c that is closest before pos, or
// of the first one when none precedes it (for example, a site earlier in a loop), the argument
// that held c there, and the class the site invalidates: c, or for a phi class, which has no
// site of its own, a class flowing into it. Every invalid class traces back to at least one
// site, because only sites invalidate.
func (a *Analysis) siteFor(c int, pos token.Pos) (int, int, int) {
	for _, class := range a.sourceClasses(c) {
		if site, arg, ok := a.nearestSite(class, pos); ok {
			return site, arg, class
		}
	}

	return 0, a.siteArgs[0][0], a.siteClasses[0][0]
}

// nearestSite returns the index of the site invalidating class c that is closest before pos,
// or of the first such site when none precedes pos.
func (a *Analysis) nearestSite(c int, pos token.Pos) (int, int, bool) {
	best, bestArg := -1, -1
	first, firstArg := -1, -1

	for i, classes := range a.siteClasses {
		for j, sc := range classes {
			if sc != c {
				continue
			}

			if first < 0 {
				first, firstArg = i, a.siteArgs[i][j]
			}

			p := a.Sites[i].Call.Pos()
			if p < pos && (best < 0 || p > a.Sites[best].Call.Pos()) {
				best, bestArg = i, a.siteArgs[i][j]
			}
		}
	}

	if best < 0 {
		best, bestArg = first, firstArg
	}

	if best < 0 {
		return 0, 0, false
	}

	return best, bestArg, true
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
// that the caller must not use it afterwards. A type parameter does not: whether it shares
// memory depends on the instantiation.
func sharesMemory(t types.Type) bool {
	if isTypeParam(t) {
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

func isNilConst(v ssa.Value) bool {
	c, ok := v.(*ssa.Const)
	return ok && c.IsNil()
}

func isNilComparison(op *ssa.BinOp) bool {
	if op.Op != token.EQL && op.Op != token.NEQ {
		return false
	}

	return isNilConst(op.X) || isNilConst(op.Y)
}

func isTypeParam(t types.Type) bool {
	_, ok := t.(*types.TypeParam)
	return ok
}
