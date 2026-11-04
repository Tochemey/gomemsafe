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

// Package escape finds how the memory a function's parameters and free variables refer to
// leaves the function: kept beyond the call, or shared with a result. It is what the summary
// analyzer exports as the Retains and Returns parts of a function's fact, and what MS502
// reports for a borrowed parameter.
//
// The analysis is flow-insensitive: a value escapes if it escapes on any path. The memory
// reachable from a root is followed through conversions, fields, elements, loads of
// memory-carrying values, phis, the local variables that held it, the memory the function made
// that it was stored into, the closures that captured it, and the results of callees whose
// summary says the result shares the argument's memory. It escapes when stored into memory the
// function did not make, sent on a channel, appended to such a slice, passed to a goroutine or
// captured by a function literal one starts, returned, passed to panic, or passed to a callee
// that keeps it: one whose summary says so, a consuming parameter, or an interface method or
// function value, which may do anything, unless a known effect says it keeps nothing.
//
// Memory the function made is an allocation, a make, a nil slice or map, and the slices,
// appends, and phis of only such values: storing into it keeps nothing beyond the call by
// itself. Memory stored into memory of the one root it came from reaches nothing new, as in
// h.items = append(h.items, h.t); stored into memory that may be another root's, it escapes.
// An error loaded from memory is taken to be a value of its own, as errors are by convention,
// so returning a field of type error is no escape of the memory it was loaded from.
//
// Function literals are analysed with their free variables as roots, so that what a literal
// keeps counts for the function that made it. A literal's store into a variable it captured is
// a store into the variable of the function that made it, which is followed there.
package escape

import (
	"go/types"
	"slices"

	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/effect"
	"github.com/tochemey/gomemsafe/internal/invalidation"
)

// maxRoots bounds the roots one function is analysed with: the bits of a uint64.
const maxRoots = 64

// Kind is how memory escapes.
type Kind int

const (
	// StoredIntoField is a store into a field of memory the function did not allocate.
	StoredIntoField Kind = iota
	// StoredIntoElement is a store into an element of such memory, or a copy into it.
	StoredIntoElement
	// StoredIntoMap is a store into a map the function did not make, as a key or a value.
	StoredIntoMap
	// StoredIntoGlobal is a store into a global, or into a field or an element of one.
	StoredIntoGlobal
	// StoredThroughPointer is a store through a pointer the function did not allocate.
	StoredThroughPointer
	// Appended is an append to a slice the function did not make.
	Appended
	// Sent is a send on a channel the function did not make, in a select or not.
	Sent
	// Returned is a return of the value.
	Returned
	// PassedToGoroutine is an argument of a go statement.
	PassedToGoroutine
	// CapturedByGoroutine is a value a function literal captures, started by a go statement.
	CapturedByGoroutine
	// PassedToKeeper is an argument of a call whose callee's summary keeps it.
	PassedToKeeper
	// PassedToConsumer is an argument passed to a consuming parameter.
	PassedToConsumer
	// PassedToUnknown is an argument of an interface method or a function value, which may
	// keep it.
	PassedToUnknown
	// PassedToPanic is the value of a panic, which a recover may keep.
	PassedToPanic
	// CapturedByWideLiteral is a value captured by a function literal with more parameters
	// and captured variables than one analysis follows, which may keep it.
	CapturedByWideLiteral
)

// Lookup answers what callees do with their arguments, the receiver first: the summary
// analyzer's Result, which reads facts and contracts.
type Lookup interface {
	// Retains returns the arguments whose memory fn keeps beyond the call, outside its results.
	Retains(fn *types.Func) effect.ArgSet
	// Returns returns the arguments whose memory a result of fn may share.
	Returns(fn *types.Func) effect.ArgSet
	// Consumes returns the arguments whose ownership fn takes.
	Consumes(fn *types.Func) effect.ArgSet
}

// Escape is one place where memory reachable from a root leaves the function.
type Escape struct {
	// Instr is the instruction, which is inside a function literal of the function when the
	// literal captured the memory.
	Instr ssa.Instruction
	// At is where Value is used: Instr, or, for an argument packed into a variadic call's
	// slice, the store that packs it, which go/ssa places at the argument.
	At ssa.Instruction
	// Value is the operand carrying the memory at At.
	Value ssa.Value
	Kind  Kind
	// Callee is the function called, for the kinds that pass the value to one; nil for a
	// function value.
	Callee *types.Func
	// Arg is the callee's argument index the value is passed as, the receiver first.
	Arg int
	// roots are the roots whose memory the value carries.
	roots uint64
}

// Analysis is the escapes of one function's roots: its parameters, the receiver first, then
// its free variables, each a bit of a set.
type Analysis struct {
	fn     *ssa.Function
	lookup Lookup
	// roots has an entry per parameter and free variable, nil for one that carries no memory.
	roots []ssa.Value
	// unbounded is set when the roots do not fit a set: everything is taken to escape.
	unbounded bool
	// sets maps each value that carries a root's memory to the roots.
	sets map[ssa.Value]uint64
	work []ssa.Value
	// escapes lists the escapes found, in discovery order; at finds one by what it is.
	escapes []Escape
	at      map[escapeKey]int
	// pending holds the stores into containers the function did not make, decided once every
	// value's roots are known: memory stored into memory of its own root reaches nothing new.
	pending []kept
	// local is the memory the function made, computed on first use.
	local map[ssa.Value]bool
	// cells maps each free variable the function stores into to the roots of what it stores:
	// the function that made the literal stores them into its own variable.
	cells map[int]uint64
	// retained and returned are the roots escaping other than by a return, and by one.
	retained, returned uint64
	// literals memoises the analyses of the function literals, shared down the nesting.
	literals map[*ssa.Function]*Analysis
}

// escapeKey tells escapes apart: the same value may be found escaping at the same instruction
// through several roots, which is one escape.
type escapeKey struct {
	instr ssa.Instruction
	value ssa.Value
	kind  Kind
	arg   int
}

// kept is a value put into a container the function did not allocate, with the roots the
// value carried when it was found.
type kept struct {
	instr, at ssa.Instruction
	container ssa.Value
	value     ssa.Value
	kind      Kind
	bits      uint64
}

// Analyze finds how the memory reachable from each parameter and free variable of fn escapes
// it. A function without a body, implemented in assembly, keeps nothing.
func Analyze(fn *ssa.Function, lookup Lookup) *Analysis {
	return analyze(fn, lookup, make(map[*ssa.Function]*Analysis))
}

// Shares reports whether a value of type t carries memory a copy of it would share: a
// pointer, a slice, a map, a channel, a function, an interface, an unsafe.Pointer, or a struct
// or an array holding one. A type parameter does not: it depends on the instantiation.
func Shares(t types.Type) bool {
	if invalidation.SharesMemory(t) {
		return true
	}

	switch u := t.Underlying().(type) {
	case *types.Struct:
		for i := range u.NumFields() {
			if Shares(u.Field(i).Type()) {
				return true
			}
		}
	case *types.Array:
		return Shares(u.Elem())
	}

	return false
}

// Retained returns the parameters whose memory escapes other than through a result.
func (a *Analysis) Retained() effect.ArgSet { return a.params(a.retained) }

// Returned returns the parameters whose memory a result may share. A function whose roots did
// not fit a set shares none with results that carry no memory.
func (a *Analysis) Returned() effect.ArgSet {
	results := a.fn.Signature.Results()

	for i := range results.Len() {
		if Shares(results.At(i).Type()) {
			return a.params(a.returned)
		}
	}

	return 0
}

// Escapes returns the escapes of the memory of parameter arg, in source order.
func (a *Analysis) Escapes(arg int) []Escape {
	var out []Escape

	for _, e := range a.escapes {
		if arg < maxRoots && e.roots&(1<<uint(arg)) != 0 {
			out = append(out, e)
		}
	}

	slices.SortStableFunc(out, func(x, y Escape) int { return int(x.Instr.Pos() - y.Instr.Pos()) })

	return out
}

func analyze(fn *ssa.Function, lookup Lookup, literals map[*ssa.Function]*Analysis) *Analysis {
	a := &Analysis{
		fn: fn, lookup: lookup, sets: make(map[ssa.Value]uint64), at: make(map[escapeKey]int), literals: literals,
		cells: make(map[int]uint64),
	}
	literals[fn] = a

	for _, p := range fn.Params {
		a.root(p)
	}

	for _, v := range fn.FreeVars {
		a.root(v)
	}

	if a.unbounded {
		return a
	}

	for i, r := range a.roots {
		if r != nil {
			a.add(r, 1<<uint(i))
		}
	}

	for len(a.work) > 0 {
		v := a.work[len(a.work)-1]
		a.work = a.work[:len(a.work)-1]

		// Every value that carries memory is a parameter, a free variable, or an instruction,
		// which all have referrers.
		for _, r := range *v.Referrers() {
			a.use(r, v, a.sets[v])
		}
	}

	for _, k := range a.pending {
		bits := k.bits

		// Into memory of exactly one root, that root's own memory reaches nothing new; into
		// memory that may be any of several roots, each value may reach another root.
		if c := a.sets[k.container]; c != 0 && c&(c-1) == 0 {
			bits &^= c
		}

		if bits != 0 {
			a.escape(k.instr, k.at, k.value, k.kind, nil, 0, bits)
		}
	}

	return a
}

// root adds v as the next root, nil when it carries no memory.
func (a *Analysis) root(v ssa.Value) {
	if len(a.roots) == maxRoots {
		a.unbounded = true
		return
	}

	if !Shares(v.Type()) {
		v = nil
	}

	a.roots = append(a.roots, v)
}

// params returns the parameters among the roots in bits: every memory-carrying one when the
// roots did not fit a set.
func (a *Analysis) params(bits uint64) effect.ArgSet {
	var out effect.ArgSet

	for i, p := range a.fn.Params {
		if i >= maxRoots {
			break
		}

		if a.unbounded && Shares(p.Type()) || !a.unbounded && bits&(1<<uint(i)) != 0 {
			out |= 1 << uint(i)
		}
	}

	return out
}

// add records that v carries the memory of the roots in bits, and queues it for its uses.
func (a *Analysis) add(v ssa.Value, bits uint64) {
	if bits == 0 || a.sets[v]&bits == bits {
		return
	}

	a.sets[v] |= bits
	a.work = append(a.work, v)
}

// escape records that v, carrying the roots in bits, escapes at instr, used at at.
func (a *Analysis) escape(instr, at ssa.Instruction, v ssa.Value, kind Kind, callee *types.Func, arg int, bits uint64) {
	key := escapeKey{instr, v, kind, arg}

	i, seen := a.at[key]
	if !seen {
		i = len(a.escapes)
		a.at[key] = i
		a.escapes = append(a.escapes, Escape{Instr: instr, At: at, Value: v, Kind: kind, Callee: callee, Arg: arg})
	}

	a.escapes[i].roots |= bits

	if kind == Returned {
		a.returned |= bits
	} else {
		a.retained |= bits
	}
}

// literal returns the analysis of lit, a function literal or a synthetic wrapper of fn, with
// lit's parameters and free variables as roots.
func (a *Analysis) literal(lit *ssa.Function) *Analysis {
	if sub, ok := a.literals[lit]; ok {
		return sub
	}

	return analyze(lit, a.lookup, a.literals)
}
