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

package summary

import (
	"go/types"
	"slices"

	"github.com/tochemey/gomemsafe/internal/effect"
	"github.com/tochemey/gomemsafe/internal/prefilter"
)

// Syncs reports whether a call of fn may synchronise with another goroutine, so that the
// goroutine's writes before its matching operation are visible after the call.
func (r *Result) Syncs(fn *types.Func) bool {
	if effect.SyncKnown(fn) {
		return effect.Synchronizes(fn)
	}

	if fn.Origin().Pkg() == r.pass.Pkg {
		return r.syncs[fn.Origin()]
	}

	return r.factOf(fn).Syncs
}

// synchronize records which of funcs may synchronise: those that communicate on a channel, call
// an interface method or a function value, or call a function of another package that
// synchronises, and then, to a fixpoint, every caller of one that does.
func (r *Result) synchronize(funcs []*prefilter.Func, callers map[*types.Func][]*prefilter.Func) {
	var work []*prefilter.Func

	for _, f := range funcs {
		if effect.SyncKnown(f.Obj) {
			continue
		}

		if f.Communicates || f.CallsDynamic || slices.ContainsFunc(f.Callees, r.syncsElsewhere) {
			r.syncs[f.Obj] = true
			work = append(work, f)
		}
	}

	for len(work) > 0 {
		f := work[len(work)-1]
		work = work[:len(work)-1]

		for _, caller := range callers[f.Obj] {
			if !r.syncs[caller.Obj] && !effect.SyncKnown(caller.Obj) {
				r.syncs[caller.Obj] = true
				work = append(work, caller)
			}
		}
	}
}

// syncsElsewhere reports whether fn, declared in another package, may synchronise.
func (r *Result) syncsElsewhere(fn *types.Func) bool {
	return fn.Origin().Pkg() != r.pass.Pkg && r.Syncs(fn)
}
