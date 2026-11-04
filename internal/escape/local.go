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

package escape

import (
	"slices"

	"golang.org/x/tools/go/ssa"
)

// intoOwn stores memory of the roots in bits into base, the start of a container, when the
// container is the function's own: memory it made, which then carries the roots, a nil slice
// or map, where nothing is stored, or a variable a literal captured, which belongs to the
// function that made the literal. It reports whether it did.
func (a *Analysis) intoOwn(base ssa.Value, bits uint64) bool {
	switch x := base.(type) {
	case *ssa.Const:
		return true
	case *ssa.FreeVar:
		a.cells[slices.Index(a.fn.FreeVars, x)] |= bits
		return true
	}

	if a.made()[base] {
		a.add(base, bits)
		return true
	}

	return false
}

// intoCells stores what the literal sub analysed stores into its captured variables, from its
// root, into the variables mc binds them to: the roots in bits go where the root went. go/ssa
// binds a captured variable as its address: an allocation of this function, or, when this
// function is a literal too, one of its own free variables, both its own memory.
func (a *Analysis) intoCells(mc *ssa.MakeClosure, sub *Analysis, root int, bits uint64) {
	for k, stored := range sub.cells {
		if stored&(1<<uint(root)) != 0 {
			a.intoOwn(baseOf(mc.Bindings[k]), bits)
		}
	}
}

// made returns the memory the function made: its allocations and makes, and the slices,
// appends, and phis of only such memory or nil. A phi on a cycle is made by the function
// unless a value entering the cycle is not, the greatest such set, found by removing what has
// an input from elsewhere until nothing changes.
func (a *Analysis) made() map[ssa.Value]bool {
	if a.local != nil {
		return a.local
	}

	a.local = make(map[ssa.Value]bool)

	var derived []ssa.Value

	for _, b := range a.fn.Blocks {
		for _, instr := range b.Instrs {
			switch x := instr.(type) {
			case *ssa.Alloc, *ssa.MakeSlice, *ssa.MakeMap, *ssa.MakeChan:
				a.local[x.(ssa.Value)] = true
			case *ssa.Phi, *ssa.Slice:
				a.local[x.(ssa.Value)] = true
				derived = append(derived, x.(ssa.Value))
			case *ssa.Call:
				if isAppend(x) {
					a.local[x] = true
					derived = append(derived, x)
				}
			}
		}
	}

	for changed := true; changed; {
		changed = false

		for _, v := range derived {
			if a.local[v] && !a.inputsMade(v) {
				a.local[v] = false
				changed = true
			}
		}
	}

	return a.local
}

// inputsMade reports whether the values v, a phi, a slice, or an append, is made from are all
// the function's own memory or nil.
func (a *Analysis) inputsMade(v ssa.Value) bool {
	var inputs []ssa.Value

	switch x := v.(type) {
	case *ssa.Phi:
		inputs = x.Edges
	case *ssa.Slice:
		inputs = []ssa.Value{x.X}
	case *ssa.Call:
		inputs = x.Call.Args[:1]
	}

	for _, in := range inputs {
		if _, isConst := in.(*ssa.Const); !isConst && !a.local[in] {
			return false
		}
	}

	return true
}

// isAppend reports whether call calls the append builtin.
func isAppend(call *ssa.Call) bool {
	b, ok := call.Call.Value.(*ssa.Builtin)
	return ok && b.Name() == builtinAppend
}
