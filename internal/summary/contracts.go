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

	"github.com/tochemey/gomemsafe/internal/directive"
	"github.com/tochemey/gomemsafe/internal/effect"
)

// InterfaceContracts returns the interface methods with a contract, in this package and the
// packages it depends on, sorted by name. It reads every imported fact, so only checks that run
// on the packages being analysed call it, never on dependencies.
func (r *Result) InterfaceContracts() []*types.Func {
	r.scanFacts()

	return r.interfaceContracts
}

// contractFact returns the fact that the contract c alone declares. An interface method has no
// body to derive anything from, so what its contract leaves out stays as for any call of an
// unknown method: it may keep every argument that the contract does not borrow. The receiver,
// which a contract cannot name, is among them, so a contract's Retains is never empty, which
// tells a contract's method from one with no contract. Its first result shares the memory of the
// argument it is a view into, even one the contract borrows.
func contractFact(c *directive.Contract) Fact {
	fact := withContract(Fact{}, c)
	if directive.IsInterfaceMethod(c.Func) {
		args := uint(c.Func.Signature().Params().Len() + 1)
		fact.Retains = (effect.ArgSet(1)<<args - 1) &^ c.Borrows

		if c.View.EndedBy != 0 {
			fact.Returns = 1 << uint(c.View.Arg)
		}
	}

	return fact
}

// withContract returns fact with the contract c added: the declared view replaces a derived
// one, every argument c says a call invalidates is all events, the arguments c says a call
// consumes are consumed, and kept, and the arguments c says the function borrows are not kept,
// whatever its body does.
func withContract(fact Fact, c *directive.Contract) Fact {
	if c.View.EndedBy != 0 {
		fact.View = c.View
	}

	fact.Consumes |= c.Consumes
	fact.Retains = (fact.Retains | c.Consumes) &^ c.Borrows

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
