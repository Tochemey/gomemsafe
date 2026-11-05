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
// (MS302), the parameters whose ownership it takes (MS501), and the parameters whose memory it
// keeps beyond the call, or shares with a result, on any path (MS502). It also records whether
// a call of the function may synchronise with another goroutine (MS401), and the lock contract
// its checklocks annotations declare (MS403). Callers in other packages read the fact instead
// of the callee's body, the way Rust's borrow checker trusts signatures.
//
// An interface method's contract is its fact too, so that calls of the method, in any package,
// read it as they read a function's. It has no body: what the contract leaves out is what an
// unknown callee may do, and every other part comes from the contract alone.
//
// A function's contract, declared with //memsafe:viewof, //memsafe:invalidates,
// //memsafe:consumes, and //memsafe:borrows, is part of its fact as declared, whatever its
// body does; what the summary derives from the body is added to it, except that a borrowed
// parameter is never kept: callers trust the declaration, and MS502 checks the body. Ownership
// is never derived: only a contract declares it. Known effects (package effect) take
// precedence over both. A type declared with
// //memsafe:owned gets an OwnedFact, so that its values move in every package. A field or a
// global that checklocks annotations guard, or name as a lock, gets a GuardFact, so that
// accesses to it are checked in every package.
//
// A function synchronises when its body, its function literals included, communicates on a
// channel or calls a function that may synchronise: one package effect knows, such as
// sync.WaitGroup.Wait, one whose summary says so, such as errgroup.Group.Wait, or an interface
// method or a function value, which could be either. What the functions of packages sync and
// sync/atomic do is known, never derived: their code runs atomic operations even where, like
// Mutex.Unlock, it completes no edge in the caller. This may-property suppresses reports, so it
// errs towards true: a function literal that is never called still counts.
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
	"go/types"
	"reflect"
	"slices"
	"sort"
	"sync"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/directive"
	"github.com/tochemey/gomemsafe/internal/effect"
	"github.com/tochemey/gomemsafe/internal/escape"
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
	FactTypes:  []analysis.Fact{new(Fact), new(OwnedFact), new(GuardFact)},
	ResultType: reflect.TypeFor[*Result](),
}

// Result is what checks need from the summaries of one package. It implements
// invalidation.Lookup. The checks that read it run in parallel goroutines, the way unitchecker
// runs the analyzers of one package, so the caches of imported facts are filled under mu; what
// run computes is complete before any check starts and is read without it.
type Result struct {
	// Triggered lists the function declarations, in source order, that call at least one
	// invalidating function directly or inside a function literal.
	Triggered []*prefilter.Func

	pass  *analysis.Pass
	dir   *directive.Result
	local map[*types.Func]Fact
	syncs map[*types.Func]bool

	mu            sync.Mutex
	imported      map[*types.Func]Fact
	ownedImported map[*types.TypeName]bool
	guardImported map[types.Object]GuardFact

	factScan           sync.Once
	factAnnotated      bool
	interfaceContracts []*types.Func
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

// Consumes returns the call arguments whose ownership a call of fn takes.
func (r *Result) Consumes(fn *types.Func) effect.ArgSet { return r.factOf(fn).Consumes }

// Retains returns the call arguments whose memory a call of fn may keep beyond the call,
// outside its results.
func (r *Result) Retains(fn *types.Func) effect.ArgSet {
	if retains, _, known := effect.Kept(fn); known {
		return retains
	}

	return r.factOf(fn).Retains
}

// Returns returns the call arguments whose memory a result of fn may share.
func (r *Result) Returns(fn *types.Func) effect.ArgSet {
	if _, returns, known := effect.Kept(fn); known {
		return returns
	}

	return r.factOf(fn).Returns
}

// Owned reports whether t is a type whose values move, or a pointer to one: a named type, or an
// instance of a generic one, declared with //memsafe:owned in any package.
func (r *Result) Owned(t types.Type) bool {
	t = types.Unalias(t)
	if ptr, ok := t.(*types.Pointer); ok {
		t = types.Unalias(ptr.Elem())
	}

	named, ok := t.(*types.Named)
	if !ok {
		return false
	}

	tn := named.Obj()
	if tn.Pkg() == r.pass.Pkg {
		return r.dir.IsOwned(tn)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	owned, seen := r.ownedImported[tn]
	if !seen && tn.Pkg() != nil {
		owned = r.pass.ImportObjectFact(tn, new(OwnedFact))
		r.ownedImported[tn] = owned
	}

	return owned
}

// Annotated reports whether the package, or any package it depends on, declares an owned type
// or a consuming parameter, so that ownership can move at all. It reads every imported fact, so
// only checks that run on the packages being analysed call it, never on dependencies.
func (r *Result) Annotated() bool {
	if len(r.dir.Owned) > 0 {
		return true
	}

	for _, c := range r.dir.Contracts {
		if c.Consumes != 0 {
			return true
		}
	}

	r.scanFacts()

	return r.factAnnotated
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

	r.mu.Lock()
	defer r.mu.Unlock()

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
// can be one, whether it passes a parameter to something that ends views, and whether a
// memory-carrying parameter may leave it: named where the function itself may keep it, passed
// to an interface method or a function value that may, passed to a callee that keeps or
// consumes an argument, or passed to a callee that may return its memory, whose result is used
// where it could leave. Most functions do none of these and are never lowered to SSA.
func (r *Result) candidate(f *prefilter.Func) (invalidates, views, events, keeps bool) {
	for _, c := range f.Callees {
		invalidates = invalidates || r.Invalidates(c) != 0
	}

	for _, c := range f.ParamCallees {
		_, isView := r.ViewOf(c)
		views = views || isView
		events = events || r.EndsViews(c)
	}

	results := f.Obj.Signature().Results()

	keeps = f.ParamStored || f.ParamDynamic ||
		slices.ContainsFunc(f.ArgCallees, func(c *types.Func) bool { return r.Retains(c)|r.Consumes(c) != 0 }) ||
		slices.ContainsFunc(f.ResultCallees, func(c *types.Func) bool { return r.Returns(c) != 0 })

	return invalidates, views && results.Len() > 0 && invalidation.SharesMemory(results.At(0).Type()), events, keeps
}

func run(pass *analysis.Pass) (any, error) {
	pre := pass.ResultOf[prefilter.Analyzer].(*prefilter.Result)
	ssaRes := pass.ResultOf[lazyssa.Analyzer].(*lazyssa.Result)
	dir := pass.ResultOf[directive.Analyzer].(*directive.Result)

	res := &Result{
		pass: pass, dir: dir, local: make(map[*types.Func]Fact), syncs: make(map[*types.Func]bool),
		imported: make(map[*types.Func]Fact), ownedImported: make(map[*types.TypeName]bool),
		guardImported: make(map[types.Object]GuardFact),
	}

	for _, c := range dir.Contracts {
		res.local[c.Func] = contractFact(c)
	}

	_, _, contracted := dir.LockFacts()

	for _, fn := range contracted {
		fact := res.local[fn]
		fact.Locks, _ = dir.LockContractOf(fn)
		res.local[fn] = fact
	}

	callers := make(map[*types.Func][]*prefilter.Func)

	for _, f := range pre.Funcs {
		for _, c := range f.Callees {
			callers[c.Origin()] = append(callers[c.Origin()], f)
		}
	}

	res.synchronize(pre.Funcs, callers)

	// Summarise to a fixpoint: a function that starts invalidating a parameter, returning a view,
	// or ending one can make its callers in this package do the same. Every part only grows: a
	// view, once found, is made of views whose own summaries no longer change. So this terminates.
	var (
		eng  flow.Engine
		work []*prefilter.Func
	)

	queued := make(map[*prefilter.Func]bool)

	for _, f := range pre.Funcs {
		if invalidates, views, events, keeps := res.candidate(f); invalidates || views || events || keeps {
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

	for fn := range res.syncs {
		if fn.Exported() {
			fact := res.local[fn]
			fact.Syncs = true
			res.local[fn] = fact
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

	for _, tn := range dir.Owned {
		pass.ExportObjectFact(tn, &OwnedFact{Owned: true})
	}

	res.exportGuards()

	for _, f := range pre.Funcs {
		if invalidates, _, _, _ := res.candidate(f); invalidates {
			res.Triggered = append(res.Triggered, f)
		}
	}

	return res, nil
}

// summarize returns f's summary: what its body does on every path to a normal return, with its
// contract added. Known effects need no summary.
func (r *Result) summarize(ssaRes *lazyssa.Result, f *prefilter.Func, dir *directive.Result, eng *flow.Engine) Fact {
	var fact Fact

	invalidates, views, events, keeps := r.candidate(f)
	if knownViewEffect(f.Obj) {
		views, events = false, false
	}

	fn := ssaRes.Func(f.Obj)

	if fn != nil && len(fn.Params) <= 64 && (invalidates || views || events) {
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

	// The escape analysis takes any number of parameters: those it cannot follow escape.
	if fn != nil && keeps {
		a := escape.Analyze(fn, r)
		fact.Retains, fact.Returns = a.Retained(), a.Returned()
	}

	if c, ok := dir.ContractOf(f.Obj); ok {
		fact = withContract(fact, c)
	}

	fact.Locks, _ = dir.LockContractOf(f.Obj)

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
	return a.Invalidates == b.Invalidates && a.View == b.View && slices.Equal(a.Events, b.Events) &&
		a.Consumes == b.Consumes && a.Retains == b.Retains && a.Returns == b.Returns && a.Syncs == b.Syncs &&
		a.Locks.Equal(b.Locks)
}

// knownViewEffect reports whether package effect knows fn's views and events, which then take
// precedence over any summary: the standard library's are documented, and its code may do more
// than the documentation promises.
func knownViewEffect(fn *types.Func) bool {
	_, isView := effect.ViewOf(fn)
	return isView || effect.EventsOf(fn) != 0
}
