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

// Package summary computes, for every function, which of its parameters it invalidates on
// every path to a normal return, and exports that as a Fact. Callers in other packages read
// the fact instead of the callee's body, the way Rust's borrow checker trusts signatures.
//
// It is the only gomemsafe analyzer that exports facts, so it is the only one that does work
// on every dependency: checks that require it also run there, but return immediately. Within
// a package it iterates to a fixpoint, because a function that starts invalidating a parameter
// can make its callers invalidate theirs. Facts are deterministic: they hold only bitsets, a
// function that invalidates nothing gets no fact, and a test checks that a body-only edit
// leaves the facts file byte-identical.
//
// Result tells checks which function declarations call anything that invalidates
// (Triggered), and resolves any callee to its invalidated arguments (Invalidates).
package summary

import (
	"fmt"
	"go/types"
	"reflect"
	"sort"

	"golang.org/x/tools/go/analysis"

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
	Requires:   []*analysis.Analyzer{prefilter.Analyzer, lazyssa.Analyzer},
	Run:        run,
	FactTypes:  []analysis.Fact{new(Fact)},
	ResultType: reflect.TypeFor[*Result](),
}

// Fact records the call-argument indexes a function invalidates (receiver first for
// methods). Functions that invalidate nothing get no fact, so the common case costs nothing in
// the facts file.
type Fact struct {
	Invalidates effect.ArgSet
}

// Result is what checks need from the summaries of one package.
type Result struct {
	// Triggered lists the function declarations, in source order, that call at least one
	// invalidating function directly or inside a function literal.
	Triggered []*prefilter.Func
	// Describe words what a callee does to the arguments it invalidates, for diagnostics. It
	// is empty for callees whose effect is derived rather than known.
	Describe func(fn *types.Func) string

	lookup invalidation.Lookup
}

// AFact implements analysis.Fact.
func (*Fact) AFact() {}

func (f *Fact) String() string { return fmt.Sprintf("invalidates(%#x)", uint64(f.Invalidates)) }

// Invalidates returns the call-argument indexes fn invalidates.
func (r *Result) Invalidates(fn *types.Func) effect.ArgSet { return r.lookup(fn) }

// Lookup returns the invalidation lookup used by checks.
func (r *Result) Lookup() invalidation.Lookup { return r.lookup }

func run(pass *analysis.Pass) (any, error) {
	pre := pass.ResultOf[prefilter.Analyzer].(*prefilter.Result)
	ssa := pass.ResultOf[lazyssa.Analyzer].(*lazyssa.Result)

	local := make(map[*types.Func]effect.ArgSet)
	imported := make(map[*types.Func]effect.ArgSet)
	lookup := func(fn *types.Func) effect.ArgSet {
		fn = fn.Origin()
		if inv, ok := effect.Of(fn); ok {
			return inv.Args
		}

		if fn.Pkg() == pass.Pkg {
			return local[fn]
		}

		if bits, ok := imported[fn]; ok {
			return bits
		}

		var f Fact

		bits := effect.ArgSet(0)
		if pass.ImportObjectFact(fn, &f) {
			bits = f.Invalidates
		}

		imported[fn] = bits

		return bits
	}

	callers := make(map[*types.Func][]*prefilter.Func)

	for _, f := range pre.Funcs {
		for _, c := range f.Callees {
			callers[c.Origin()] = append(callers[c.Origin()], f)
		}
	}

	triggered := func(f *prefilter.Func) bool {
		for _, c := range f.Callees {
			if lookup(c) != 0 {
				return true
			}
		}

		return false
	}

	// Summarise to a fixpoint: a function that starts invalidating a parameter can make its
	// callers in this package invalidate theirs. Bits only grow, so this terminates.
	var (
		eng  flow.Engine
		work []*prefilter.Func
	)

	queued := make(map[*prefilter.Func]bool)

	for _, f := range pre.Funcs {
		if triggered(f) {
			work = append(work, f)
			queued[f] = true
		}
	}

	for len(work) > 0 {
		f := work[0]
		work = work[1:]
		queued[f] = false

		bits := summarize(ssa, f.Obj, lookup, &eng)
		if bits == local[f.Obj] {
			continue
		}

		local[f.Obj] = bits
		for _, caller := range callers[f.Obj] {
			if !queued[caller] {
				queued[caller] = true
				work = append(work, caller)
			}
		}
	}

	exported := make([]*types.Func, 0, len(local))
	for fn, bits := range local {
		if bits != 0 {
			exported = append(exported, fn)
		}
	}

	sort.Slice(exported, func(i, j int) bool { return exported[i].Pos() < exported[j].Pos() })

	for _, fn := range exported {
		pass.ExportObjectFact(fn, &Fact{Invalidates: local[fn]})
	}

	res := &Result{lookup: lookup, Describe: func(fn *types.Func) string {
		if inv, ok := effect.Of(fn); ok {
			return inv.Description
		}

		return ""
	}}

	for _, f := range pre.Funcs {
		if triggered(f) {
			res.Triggered = append(res.Triggered, f)
		}
	}

	return res, nil
}

// summarize returns the parameters fn invalidates on every path to a normal return.
func summarize(ssa *lazyssa.Result, obj *types.Func, lookup invalidation.Lookup, eng *flow.Engine) effect.ArgSet {
	fn := ssa.Func(obj)
	if fn == nil || len(fn.Params) > 64 {
		return 0
	}

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
