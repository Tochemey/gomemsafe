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
	"fmt"
	"go/types"
	"slices"
	"strings"

	"github.com/tochemey/gomemsafe/internal/directive"
	"github.com/tochemey/gomemsafe/internal/effect"
)

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
	// Consumes lists the arguments whose ownership the function takes.
	Consumes effect.ArgSet
	// Retains lists the arguments whose memory the function may keep beyond the call, outside
	// its results: stored, sent, started in a goroutine, or passed to a callee that keeps it.
	Retains effect.ArgSet
	// Returns lists the arguments whose memory a result may share.
	Returns effect.ArgSet
	// Syncs reports whether a call of the function may synchronise with another goroutine. Only
	// functions that other packages can name, those with exported names, export it.
	Syncs bool
	// Locks is the lock contract the function's checklocks annotations declare, if any.
	Locks *directive.LockContract
}

// OwnedFact marks a type whose values have move semantics: //memsafe:owned in its doc comment.
// Only owned types get one, so Owned is always set; gob encodes no struct without a field.
type OwnedFact struct {
	Owned bool
}

// AFact implements analysis.Fact.
func (*Fact) AFact() {}

// AFact implements analysis.Fact.
func (*OwnedFact) AFact() {}

func (*OwnedFact) String() string { return "owned" }

// String lists the parts of the summary that are set: invalidates(0x1) for the argument set,
// viewof(0: 0x7) for a view into argument 0 that events 0x7 end, ends(0: 0x4) for the events
// on argument 0, consumes(0x2) for the arguments whose ownership the function takes,
// retains(0x1) and returns(0x1) for the arguments whose memory it keeps or shares with a
// result, and syncs for a function that may synchronise.
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

	if f.Consumes != 0 {
		parts = append(parts, fmt.Sprintf("consumes(%#x)", uint64(f.Consumes)))
	}

	if f.Retains != 0 {
		parts = append(parts, fmt.Sprintf("retains(%#x)", uint64(f.Retains)))
	}

	if f.Returns != 0 {
		parts = append(parts, fmt.Sprintf("returns(%#x)", uint64(f.Returns)))
	}

	if f.Syncs {
		parts = append(parts, "syncs")
	}

	if !f.Locks.Empty() {
		parts = append(parts, lockString(f.Locks))
	}

	return strings.Join(parts, " ")
}

// scanFacts reads every fact of this package and its dependencies once, for what checks ask of
// them all: whether any declares ownership, and which interface methods have a contract. Every
// Fact is on a function.
func (r *Result) scanFacts() {
	r.factScan.Do(func() {
		for _, f := range r.pass.AllObjectFacts() {
			switch fact := f.Fact.(type) {
			case *OwnedFact:
				r.factAnnotated = true
			case *Fact:
				r.factAnnotated = r.factAnnotated || fact.Consumes != 0

				if fn := f.Object.(*types.Func); directive.IsInterfaceMethod(fn) {
					r.interfaceContracts = append(r.interfaceContracts, fn)
				}
			}
		}

		slices.SortFunc(r.interfaceContracts, func(a, b *types.Func) int {
			return strings.Compare(a.FullName(), b.FullName())
		})
	})
}
