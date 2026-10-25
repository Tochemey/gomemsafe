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

// Package summary computes, for every function, what it does to its arguments on every path to
// a normal return, and exports that as a Fact: the parameters it takes back (MS301), the
// parameter its first result is a view into, and the events it is on each parameter's buffer
// (MS302). Callers in other packages read the fact instead of the callee's body, the way Rust's
// borrow checker trusts signatures.
//
// A function's view contract, declared with //memsafe:viewof and //memsafe:invalidates, is part
// of its fact as declared, whatever its body does; what the summary derives from the body is
// added to it. Known effects (package effect) take precedence over both.
//
// It is the only gomemsafe analyzer that exports facts, so it is the only one that does work
// on every dependency: checks that require it also run there, but return immediately. Within
// a package it iterates to a fixpoint, because a function that starts invalidating a parameter,
// returning a view, or ending one can make its callers do the same. Facts are deterministic:
// they hold only bitsets and argument indexes, a function with an empty summary gets no fact, and
// a test checks that a body-only edit leaves the facts file byte-identical.
//
// Result tells checks which function declarations call anything that invalidates
// (Triggered), and resolves any callee to what it does to its arguments (Lookup).
package summary

import (
	"fmt"
	"go/types"
	"reflect"
	"slices"
	"sort"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/directive"
	"github.com/tochemey/gomemsafe/internal/effect"
	"github.com/tochemey/gomemsafe/internal/flow"
	"github.com/tochemey/gomemsafe/internal/invalidation"
	"github.com/tochemey/gomemsafe/internal/lazyssa"
	"github.com/tochemey/gomemsafe/internal/prefilter"
)

// Analyzer is the only gomemsafe analyzer that exports facts.
var Analyzer = &analysis.Analyzer{
	Name:       "summary",
	Doc:        "computes per-function effect summaries and exports them as facts",
	Requires:   []*analysis.Analyzer{directive.Analyzer, prefilter.Analyzer, lazyssa.Analyzer},
	Run:        run,
	FactTypes:  []analysis.Fact{new(Fact)},
	ResultType: reflect.TypeFor[*Result](),
}

// Fact records what a function does to its call arguments, receiver first for methods.
// Functions with an empty summary get no fact, so the common case costs nothing in the facts
// file.
type Fact struct {
	// Invalidates lists the arguments the function takes back.
	Invalidates effect.ArgSet
	// View is the view the first result is; zero when it is none.
	View effect.View
	// Events lists, by argument, the events a call is on that argument's buffer. Trailing
	// arguments with none are left out.
	Events []effect.Event
}

// Result is what checks need from the summaries of one package. It implements
// invalidation.Lookup.
type Result struct {
	// Triggered lists the function declarations, in source order, that call at least one
	// invalidating function directly or inside a function literal.
	Triggered []*prefilter.Func

	pass     *analysis.Pass
	local    map[*types.Func]Fact
	imported map[*types.Func]Fact
}

// AFact implements analysis.Fact.
func (*Fact) AFact() {}

// String lists the parts of the summary that are set: invalidates(0x1) for the argument set,
// viewof(0: 0x7) for a view into argument 0 that events 0x7 end, and ends(0: 0x4) for the events
// on argument 0.
func (f *Fact) String() string {
	var parts []string

	if f.Invalidates != 0 {
		parts = append(parts, fmt.Sprintf("invalidates(%#x)", uint64(f.Invalidates)))
	}

	if f.View.EndedBy != 0 {
		parts = append(parts, fmt.Sprintf("viewof(%d: %#x)", f.View.Arg, uint8(f.View.EndedBy)))
	}

	var ends []string

	for i, events := range f.Events {
		if events != 0 {
			ends = append(ends, fmt.Sprintf("%d: %#x", i, uint8(events)))
		}
	}

	if len(ends) > 0 {
		parts = append(parts, "ends("+strings.Join(ends, ", ")+")")
	}

	return strings.Join(parts, " ")
}

// Invalidates returns the call-argument indexes fn invalidates.
func (r *Result) Invalidates(fn *types.Func) effect.ArgSet {
	if inv, ok := effect.Of(fn); ok {
		return inv.Args
	}

	return r.factOf(fn).Invalidates
}

// ViewOf returns the view fn's first result is, if it is one.
func (r *Result) ViewOf(fn *types.Func) (effect.View, bool) {
	if knownViewEffect(fn) {
		return effect.ViewOf(fn)
	}

	view := r.factOf(fn).View

	return view, view.EndedBy != 0
}

// EventsOn returns the events a call of fn is on its call argument arg.
func (r *Result) EventsOn(fn *types.Func, arg int) effect.Event {
	if knownViewEffect(fn) {
		if arg == 0 {
			return effect.EventsOf(fn)
		}

		return 0
	}

	if events := r.factOf(fn).Events; arg < len(events) {
		return events[arg]
	}

	return 0
}

// EndsViews reports whether a call of fn ends the views of any of its arguments.
func (r *Result) EndsViews(fn *types.Func) bool {
	if knownViewEffect(fn) {
		return effect.EventsOf(fn) != 0
	}

	return len(r.factOf(fn).Events) > 0
}

// Describe words what fn does to the arguments it invalidates, for diagnostics. It is empty for
// callees whose effect is derived rather than known.
func (r *Result) Describe(fn *types.Func) string {
	if inv, ok := effect.Of(fn); ok {
		return inv.Description
	}

	return ""
}

// Lookup returns the lookup checks pass to package invalidation.
func (r *Result) Lookup() invalidation.Lookup { return r }

// factOf returns the summary of fn computed in this package, or imported from fn's own.
func (r *Result) factOf(fn *types.Func) Fact {
	fn = fn.Origin()

	if fn.Pkg() == r.pass.Pkg {
		return r.local[fn]
	}

	if f, ok := r.imported[fn]; ok {
		return f
	}

	var f Fact

	r.pass.ImportObjectFact(fn, &f)
	r.imported[fn] = f

	return f
}

// candidate reports what is worth computing for f: whether it calls something that invalidates,
// whether it passes a parameter to something whose result is a view and has a first result that
// can be one, and whether it passes a parameter to something that ends views. Most functions do
// none of these and are never lowered to SSA.
func (r *Result) candidate(f *prefilter.Func) (invalidates, views, events bool) {
	for _, c := range f.Callees {
		invalidates = invalidates || r.Invalidates(c) != 0
	}

	for _, c := range f.ParamCallees {
		_, isView := r.ViewOf(c)
		views = views || isView
		events = events || r.EndsViews(c)
	}

	results := f.Obj.Signature().Results()

	return invalidates, views && results.Len() > 0 && invalidation.SharesMemory(results.At(0).Type()), events
}

func run(pass *analysis.Pass) (any, error) {
	pre := pass.ResultOf[prefilter.Analyzer].(*prefilter.Result)
	ssaRes := pass.ResultOf[lazyssa.Analyzer].(*lazyssa.Result)
	dir := pass.ResultOf[directive.Analyzer].(*directive.Result)

	res := &Result{pass: pass, local: make(map[*types.Func]Fact), imported: make(map[*types.Func]Fact)}

	for _, c := range dir.Contracts {
		res.local[c.Func] = withContract(Fact{}, c)
	}

	callers := make(map[*types.Func][]*prefilter.Func)

	for _, f := range pre.Funcs {
		for _, c := range f.Callees {
			callers[c.Origin()] = append(callers[c.Origin()], f)
		}
	}

	// Summarise to a fixpoint: a function that starts invalidating a parameter, returning a view,
	// or ending one can make its callers in this package do the same. Every part only grows: a
	// view, once found, is made of views whose own summaries no longer change. So this terminates.
	var (
		eng  flow.Engine
		work []*prefilter.Func
	)

	queued := make(map[*prefilter.Func]bool)

	for _, f := range pre.Funcs {
		if invalidates, views, events := res.candidate(f); invalidates || views || events {
			work = append(work, f)
			queued[f] = true
		}
	}

	for len(work) > 0 {
		f := work[0]
		work = work[1:]
		queued[f] = false

		fact := res.summarize(ssaRes, f, dir, &eng)
		if equal(fact, res.local[f.Obj]) {
			continue
		}

		res.local[f.Obj] = fact
		for _, caller := range callers[f.Obj] {
			if !queued[caller] {
				queued[caller] = true
				work = append(work, caller)
			}
		}
	}

	exported := make([]*types.Func, 0, len(res.local))
	for fn, fact := range res.local {
		if !equal(fact, Fact{}) {
			exported = append(exported, fn)
		}
	}

	sort.Slice(exported, func(i, j int) bool { return exported[i].Pos() < exported[j].Pos() })

	for _, fn := range exported {
		fact := res.local[fn]
		pass.ExportObjectFact(fn, &fact)
	}

	for _, f := range pre.Funcs {
		if invalidates, _, _ := res.candidate(f); invalidates {
			res.Triggered = append(res.Triggered, f)
		}
	}

	return res, nil
}

// summarize returns f's summary: what its body does on every path to a normal return, with its
// contract added. Known effects need no summary.
func (r *Result) summarize(ssaRes *lazyssa.Result, f *prefilter.Func, dir *directive.Result, eng *flow.Engine) Fact {
	var fact Fact

	invalidates, views, events := r.candidate(f)
	if knownViewEffect(f.Obj) {
		views, events = false, false
	}

	if fn := ssaRes.Func(f.Obj); fn != nil && len(fn.Params) <= 64 && (invalidates || views || events) {
		if invalidates {
			fact.Invalidates = invalidatedAtReturn(fn, r, eng)
		}

		if views {
			fact.View, _ = invalidation.ViewAtReturn(fn, r)
		}

		if events {
			fact.Events = trimEvents(invalidation.EventsAtReturn(fn, r, eng))
		}
	}

	if c, ok := dir.ContractOf(f.Obj); ok {
		fact = withContract(fact, c)
	}

	return fact
}

// invalidatedAtReturn returns the parameters fn invalidates on every path to a normal return.
func invalidatedAtReturn(fn *ssa.Function, lookup invalidation.Lookup, eng *flow.Engine) effect.ArgSet {
	a := invalidation.Analyze(fn, lookup, eng)
	if a == nil {
		return 0
	}

	var bits effect.ArgSet

	for i, invalid := range a.InvalidAtReturn() {
		if invalid {
			bits |= 1 << uint(i)
		}
	}

	return bits
}

// withContract returns fact with the view contract c added: the declared view replaces a
// derived one, and every argument c says a call invalidates is all events.
func withContract(fact Fact, c *directive.Contract) Fact {
	if c.View.EndedBy != 0 {
		fact.View = c.View
	}

	if c.Ends == 0 {
		return fact
	}

	events := slices.Clone(fact.Events)

	for i := range 64 {
		if !c.Ends.Has(i) {
			continue
		}

		if i >= len(events) {
			events = append(events, make([]effect.Event, i+1-len(events))...)
		}

		events[i] = effect.All
	}

	fact.Events = events

	return fact
}

// trimEvents drops the trailing arguments with no events, so that a summary with none has no
// slice at all.
func trimEvents(events []effect.Event) []effect.Event {
	for len(events) > 0 && events[len(events)-1] == 0 {
		events = events[:len(events)-1]
	}

	if len(events) == 0 {
		return nil
	}

	return events
}

func equal(a, b Fact) bool {
	return a.Invalidates == b.Invalidates && a.View == b.View && slices.Equal(a.Events, b.Events)
}

// knownViewEffect reports whether package effect knows fn's views and events, which then take
// precedence over any summary: the standard library's are documented, and its code may do more
// than the documentation promises.
func knownViewEffect(fn *types.Func) bool {
	_, isView := effect.ViewOf(fn)
	return isView || effect.EventsOf(fn) != 0
}
