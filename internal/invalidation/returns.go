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
	"math/bits"

	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/effect"
	"github.com/tochemey/gomemsafe/internal/flow"
)

// eventKinds is the number of kinds of event: the bits of effect.All.
var eventKinds = bits.Len8(uint8(effect.All))

// paramEvents is the problem EventsAtReturn solves. Fact p*eventKinds+k holds after event kind
// k ran on parameter p on every path.
type paramEvents struct {
	// calls maps each call that is an event on a followed parameter to those events, resolved
	// once so that the solver's passes over a block look nothing up.
	calls map[*ssa.Call][]paramEvent
	n     int
}

// paramEvent is the events a call is on parameter param.
type paramEvent struct {
	param  int
	events effect.Event
}

// ViewAtReturn returns the view fn's first result is, when every value fn returns there is nil
// or a view into the same parameter: a view fn makes, one a callee's summary declares, or a phi
// of them, followed as AnalyzeWithViews follows them. The view is ended only by the events that
// end every one of those views. A view fn ended before returning it still points into the
// parameter's buffer, so it is still a view: a caller's later event leaves it no more valid.
func ViewAtReturn(fn *ssa.Function, lookup Lookup) (effect.View, bool) {
	if fn.Signature.Results().Len() == 0 {
		return effect.View{}, false
	}

	a := newAnalysis(fn)
	a.addViews(callsOf(fn), lookup)

	if len(a.views) == 0 {
		return effect.View{}, false
	}

	a.closeOverAliases()
	a.resolveViews()

	var view effect.View

	for _, b := range fn.Blocks {
		ret, ok := b.Instrs[len(b.Instrs)-1].(*ssa.Return)
		if !ok || isNilConst(ret.Results[0]) {
			continue
		}

		returned, ok := a.returnedView(ret.Results[0])

		switch {
		case !ok, view.EndedBy != 0 && returned.Arg != view.Arg:
			return effect.View{}, false
		case view.EndedBy == 0:
			view = returned
		default:
			view.EndedBy &= returned.EndedBy
		}

		if view.EndedBy == 0 {
			return effect.View{}, false
		}
	}

	return view, view.EndedBy != 0
}

// EventsAtReturn returns, for each parameter of fn, the events that calls in fn are on it on
// every path to a reachable return, or nil when there are none. Only a parameter passed as
// itself counts, as ownerOf follows it; a field of it does not, because ending the views of one
// field leaves the views of the others valid. A parameter whose buffer fn replaces, as in
// *b = bytes.Buffer{}, is not followed: the caller's views keep the old buffer.
func EventsAtReturn(fn *ssa.Function, lookup Lookup, eng *flow.Engine) []effect.Event {
	params := make(map[ssa.Value]int, len(fn.Params))
	for i, param := range fn.Params {
		params[param] = i
	}

	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			if st, ok := instr.(*ssa.Store); ok {
				delete(params, Base(st.Addr))
			}
		}
	}

	p := &paramEvents{calls: eventsOnParams(callsOf(fn), params, lookup), n: len(fn.Params) * eventKinds}
	if len(p.calls) == 0 {
		return nil
	}

	sol, outcome := eng.Solve(fn, p)
	if outcome != flow.Solved {
		return nil
	}

	out := make([]effect.Event, len(fn.Params))
	returns := 0

	sol.Walk(func(instr ssa.Instruction, state *flow.Bitset) {
		if _, ok := instr.(*ssa.Return); !ok {
			return
		}

		for i := range out {
			var events effect.Event

			for k := range eventKinds {
				if state.Has(i*eventKinds + k) {
					events |= 1 << uint(k)
				}
			}

			if returns == 0 {
				out[i] = events
			} else {
				out[i] &= events
			}
		}

		returns++
	})

	for _, events := range out {
		if events != 0 {
			return out
		}
	}

	return nil
}

// N implements flow.Problem.
func (p *paramEvents) N() int { return p.n }

// Phi implements flow.Problem: a phi is no call, so it changes no parameter's events.
func (*paramEvents) Phi(*ssa.Phi, func(int) *flow.Bitset, *flow.Bitset) {}

// Transfer implements flow.Problem: a call adds its events on each followed parameter it is
// passed.
func (p *paramEvents) Transfer(instr ssa.Instruction, state *flow.Bitset) {
	call, ok := instr.(*ssa.Call)
	if !ok {
		return
	}

	for _, pe := range p.calls[call] {
		for k := range eventKinds {
			if pe.events&(1<<uint(k)) != 0 {
				state.Set(pe.param*eventKinds + k)
			}
		}
	}
}

// returnedView returns the view that v, a value fn returns, is into one of fn's parameters.
func (a *Analysis) returnedView(v ssa.Value) (effect.View, bool) {
	c, tracked := a.classOf[v]
	if !tracked {
		return effect.View{}, false
	}

	vc, isView := a.viewClasses[c]
	if !isView {
		return effect.View{}, false
	}

	for i, param := range a.Fn.Params {
		if vc.owner == param {
			return effect.View{Arg: i, EndedBy: vc.endedBy}, true
		}
	}

	return effect.View{}, false
}

// eventsOnParams returns, for each of calls that is an event on one of params, the events it is
// on them. params maps each followed parameter to its index.
func eventsOnParams(calls []*ssa.Call, params map[ssa.Value]int, lookup Lookup) map[*ssa.Call][]paramEvent {
	out := make(map[*ssa.Call][]paramEvent)

	for _, call := range calls {
		callee, ok := staticCallee(call)
		if !ok {
			continue
		}

		for i, arg := range call.Common().Args {
			param, followed := params[Base(arg)]
			if !followed {
				continue
			}

			if events := lookup.EventsOn(callee, i); events != 0 {
				out[call] = append(out[call], paramEvent{param: param, events: events})
			}
		}
	}

	return out
}

// callsOf returns the calls in fn, in block order.
func callsOf(fn *ssa.Function) []*ssa.Call {
	var calls []*ssa.Call

	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			if call, ok := instr.(*ssa.Call); ok {
				calls = append(calls, call)
			}
		}
	}

	return calls
}
