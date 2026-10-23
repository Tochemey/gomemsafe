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

package invalidation

import (
	"go/types"
	"maps"
	"slices"

	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/effect"
)

// lengthBuiltins read only a slice's header, never the memory a view points into.
var lengthBuiltins = map[string]bool{"len": true, "cap": true}

// View is a call whose first result is a view into its receiver's buffer, such as
// bytes.Buffer.Bytes (package effect). The view is invalid after a later call on the same
// receiver whose events end it, such as bytes.Buffer.Reset.
//
// The receiver must be the same SSA value at both calls: a local variable, a parameter, or a
// global. A receiver loaded from memory at each call, such as a field or a variable that a
// function literal captures, is not followed: the variable could be reassigned between the
// calls. Such a bug is missed, never invented.
type View struct {
	Call   *ssa.Call
	Callee *types.Func
	// Owner is the receiver the view points into.
	Owner ssa.Value
}

// viewClass is what a class that holds a view views.
type viewClass struct {
	// view indexes the view call the class comes from, the first one for a phi.
	view    int
	owner   ssa.Value
	endedBy effect.Event
}

// addViews gives the result of each view call among calls its own class.
func (a *Analysis) addViews(calls []*ssa.Call) {
	a.viewClasses = make(map[int]viewClass)

	for _, call := range calls {
		callee, ok := staticCallee(call)
		if !ok {
			continue
		}

		endedBy, isView := effect.ViewOf(callee)
		if !isView {
			continue
		}

		result, ok := viewResult(call)
		if !ok {
			continue
		}

		owner := Base(call.Common().Args[0])
		a.viewClasses[a.class(result)] = viewClass{view: len(a.views), owner: owner, endedBy: endedBy}
		a.views = append(a.views, View{Call: call, Callee: callee, Owner: owner})
	}
}

// endViews makes views of phis whose values are all views of one owner or nil, then records
// as sites the calls among calls whose events end some views. It runs after closeOverAliases,
// which gives the phis their classes.
func (a *Analysis) endViews(calls []*ssa.Call) {
	if len(a.views) == 0 {
		return
	}

	a.addPhiViews()

	for _, call := range calls {
		callee, ok := staticCallee(call)
		if !ok {
			continue
		}

		events := effect.EventsOf(callee)
		if events == 0 {
			continue
		}

		owner := Base(call.Common().Args[0])

		var classes []int

		for c, vc := range a.viewClasses {
			if vc.owner == owner && vc.endedBy&events != 0 {
				classes = append(classes, c)
			}
		}

		if len(classes) > 0 {
			// Every class is a view of the receiver, argument 0.
			slices.Sort(classes)
			a.addSiteClasses(Site{Call: call, Callee: callee, Args: []int{0}}, classes, make([]int, len(classes)), true)
		}
	}
}

// addPhiViews makes a view of each phi class whose values are all views, nil, or such phis,
// such as a view kept across loop iterations. Phis of nested loops feed each other, so each
// phi is resolved by a search through the phis it reaches: it is a view when every value found
// is nil or a view, every view has the same owner, and that owner is defined before the phi
// (ownerPrecedes). It is ended only by events that end all of those views.
func (a *Analysis) addPhiViews() {
	phiViews := make(map[int]viewClass)

	for c, base := range a.bases {
		phi, isPhi := base.(*ssa.Phi)
		if !isPhi {
			continue
		}

		if vc, ok := a.phiView(c); ok && ownerPrecedes(vc.owner, phi) {
			phiViews[c] = vc
		}
	}

	maps.Copy(a.viewClasses, phiViews)
}

// phiView returns what the phi class c views, if it is a view: see addPhiViews.
func (a *Analysis) phiView(c int) (viewClass, bool) {
	var merged viewClass

	found := false
	seen := map[int]bool{c: true}

	for work := []int{c}; len(work) > 0; {
		phi := a.bases[work[len(work)-1]].(*ssa.Phi)
		work = work[:len(work)-1]

		for _, v := range phi.Edges {
			if isNilConst(v) {
				continue
			}

			vc, tracked := a.classOf[v]
			if !tracked {
				return viewClass{}, false
			}

			if seen[vc] {
				continue
			}

			seen[vc] = true

			if _, isPhi := a.bases[vc].(*ssa.Phi); isPhi {
				work = append(work, vc)
				continue
			}

			view, isView := a.viewClasses[vc]

			switch {
			case !isView, found && view.owner != merged.owner:
				return viewClass{}, false
			case !found:
				merged, found = view, true
			default:
				merged.endedBy &= view.endedBy
			}
		}
	}

	return merged, found
}

// ownerPrecedes reports whether owner is one value for every execution of phi: a parameter, a
// free variable, or a global, or an instruction whose block strictly dominates phi's block. An
// owner defined inside the loop that phi heads, such as a buffer declared in the loop body, is
// a new buffer on each iteration although it is the same SSA value, so a view carried round
// the loop belongs to an earlier one.
func ownerPrecedes(owner ssa.Value, phi *ssa.Phi) bool {
	instr, ok := owner.(ssa.Instruction)
	if !ok {
		return true
	}

	defined := instr.Block()

	return defined != phi.Block() && defined.Dominates(phi.Block())
}

// viewResult returns the view a call returns: the call itself, or the first value of the tuple
// it returns, such as the slice of bufio.Reader.Peek. It reports false when that value is not
// used.
func viewResult(call *ssa.Call) (ssa.Value, bool) {
	if _, isTuple := call.Type().(*types.Tuple); !isTuple {
		return call, true
	}

	for _, r := range referrersOf(call) {
		if ex, ok := r.(*ssa.Extract); ok && ex.Index == 0 {
			return ex, true
		}
	}

	return nil, false
}

// readsOnlyLength reports whether instr is len or cap, which read a view's header and not the
// memory it points into, so they are no use of an ended view.
func readsOnlyLength(instr ssa.Instruction) bool {
	call, ok := instr.(*ssa.Call)
	if !ok {
		return false
	}

	b, ok := call.Common().Value.(*ssa.Builtin)

	return ok && lengthBuiltins[b.Name()]
}
