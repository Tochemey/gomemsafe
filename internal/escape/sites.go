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
	"cmp"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/effect"
)

// Builtins whose result shares the memory of their arguments, by name as go/ssa names them.
const (
	builtinAppend     = "append"
	builtinCopy       = "copy"
	builtinWrapNilChk = "ssa:wrapnilchk"
	builtinSlice      = "Slice"
	builtinSliceData  = "SliceData"
	builtinString     = "String"
	builtinStringData = "StringData"
	builtinAdd        = "Add"
)

// varargsComment marks the array go/ssa packs a variadic call's arguments into.
const varargsComment = "varargs"

// callee is what a callee does with its arguments.
type callee struct {
	retains, returns, consumes effect.ArgSet
	// unknown is set when the callee may do anything: an interface method with no known
	// effect, a function value, or a goroutine.
	unknown bool
}

// use follows the memory v carries, of the roots in bits, into its use by instr: into another
// value that carries it, or out of the function.
func (a *Analysis) use(instr ssa.Instruction, v ssa.Value, bits uint64) {
	switch x := instr.(type) {
	case *ssa.MakeInterface, *ssa.ChangeType, *ssa.ChangeInterface, *ssa.Convert, *ssa.MultiConvert,
		*ssa.SliceToArrayPointer, *ssa.Slice, *ssa.FieldAddr, *ssa.IndexAddr, *ssa.Phi:
		a.derive(x.(ssa.Value), bits)
	case *ssa.Range:
		// The iterator's type is opaque: what it yields is decided at the extracts of Next.
		a.add(x, bits)
	case *ssa.UnOp:
		// A load through the memory, or a receive from a channel carrying it. An error loaded
		// is a value of its own.
		if x.Op == token.ARROW || x.Op == token.MUL && !isError(x.Type()) {
			a.derive(x, bits)
		}
	case *ssa.Field:
		if !isError(x.Type()) {
			a.derive(x, bits)
		}
	case *ssa.Index, *ssa.Lookup, *ssa.Next, *ssa.Extract, *ssa.TypeAssert:
		// The first operand is the value these are made from: a map's key, the second, is not.
		if ops := x.Operands(nil); *ops[0] == v {
			a.derive(x.(ssa.Value), bits)
		}
	case *ssa.MakeClosure:
		for j, b := range x.Bindings {
			if b == v {
				a.add(x, bits)
				a.capture(x, j, bits)
			}
		}
	case *ssa.Store:
		if x.Val == v {
			a.stored(x, x.Addr, v, bits)
		}
	case *ssa.MapUpdate:
		if x.Key == v || x.Value == v {
			a.kept(x, x, x.Map, v, StoredIntoMap, bits)
		}
	case *ssa.Send:
		if x.X == v {
			a.kept(x, x, x.Chan, v, Sent, bits)
		}
	case *ssa.Select:
		a.selected(x, v, bits)
	case *ssa.Return:
		a.escape(x, x, v, Returned, nil, 0, bits)
	case *ssa.Panic:
		a.escape(x, x, v, PassedToPanic, nil, 0, bits)
	case ssa.CallInstruction:
		a.called(x, v, bits)
	}
}

// derive records that v, made from a value carrying the roots in bits, carries them too when
// its type can: a tuple's elements are decided by their extracts.
func (a *Analysis) derive(v ssa.Value, bits uint64) {
	if _, isTuple := v.Type().(*types.Tuple); isTuple || Shares(v.Type()) {
		a.add(v, bits)
	}
}

// stored follows a store of v into addr: into an allocation of this function, which then
// carries the memory, or out of the function.
func (a *Analysis) stored(instr ssa.Instruction, addr, v ssa.Value, bits uint64) {
	kind := StoredThroughPointer

	switch addr.(type) {
	case *ssa.FieldAddr:
		kind = StoredIntoField
	case *ssa.IndexAddr:
		kind = StoredIntoElement
	}

	a.kept(instr, instr, addr, v, kind, bits)
}

// selected follows v into a select: sent, it escapes whichever case runs; a channel, what its
// case receives carries the memory. The received values are the tuple's elements after the
// index and the ok, one per receiving case in order.
func (a *Analysis) selected(sel *ssa.Select, v ssa.Value, bits uint64) {
	received := 0

	for _, state := range sel.States {
		switch {
		case state.Dir == types.SendOnly:
			if state.Send == v {
				a.escape(sel, sel, v, Sent, nil, 0, bits)
			}
		case state.Chan == v:
			for _, r := range *sel.Referrers() {
				if ext, ok := r.(*ssa.Extract); ok && ext.Index == 2+received {
					a.derive(ext, bits)
				}
			}
		}

		if state.Dir == types.RecvOnly {
			received++
		}
	}
}

// kept follows v, used at at, put by instr into the memory container addresses: memory the
// function made carries it afterwards, a variable a literal captured is the variable of the
// function that made the literal, and anything else keeps it beyond the call, decided once
// the container's roots are known.
func (a *Analysis) kept(instr, at ssa.Instruction, container, v ssa.Value, kind Kind, bits uint64) {
	base := baseOf(container)

	switch {
	case a.intoOwn(base, bits):
		return
	case rootedInGlobal(container):
		kind = StoredIntoGlobal
	}

	a.pending = append(a.pending, kept{instr: instr, at: at, container: base, value: v, kind: kind, bits: bits})
}

// capture follows the capture of a value carrying the roots in bits as free variable j of the
// literal mc makes: what the literal does with the free variable, other than return it, it
// does with the roots, and what it stores into the variables it captured goes into this
// function's variables.
func (a *Analysis) capture(mc *ssa.MakeClosure, j int, bits uint64) {
	lit := mc.Fn.(*ssa.Function)
	root := len(lit.Params) + j

	sub := a.literal(lit)

	if root >= maxRoots || sub.unbounded {
		a.escape(mc, mc, mc, CapturedByWideLiteral, nil, 0, bits)
		return
	}

	for _, e := range sub.escapes {
		if e.Kind != Returned && e.roots&(1<<uint(root)) != 0 {
			a.escape(e.Instr, e.At, e.Value, e.Kind, e.Callee, e.Arg, bits)
		}
	}

	a.intoCells(mc, sub, root, bits)
}

// called follows v into a call, go, or defer: as the callee, which a go statement keeps, or
// as an argument, which the callee's summary, a known effect, or the body of a function
// literal or a synthetic wrapper decides on.
func (a *Analysis) called(instr ssa.CallInstruction, v ssa.Value, bits uint64) {
	common := instr.Common()
	_, isGo := instr.(*ssa.Go)

	if common.IsInvoke() {
		args := append([]ssa.Value{common.Value}, common.Args...)
		retains, returns, known := effect.Kept(common.Method)

		a.passed(instr, common.Method, args, v, bits, callee{retains: retains, returns: returns, unknown: !known})

		return
	}

	if common.Value == v {
		if _, isClosure := v.(*ssa.MakeClosure); isClosure && isGo {
			a.escape(instr, instr, v, CapturedByGoroutine, nil, 0, bits)
		} else if isGo {
			a.escape(instr, instr, v, PassedToGoroutine, nil, 0, bits)
		}
	}

	switch fn := common.Value.(type) {
	case *ssa.Builtin:
		a.builtin(instr, fn, v, bits)
	case *ssa.Function:
		obj, _ := fn.Object().(*types.Func)
		a.passed(instr, obj, common.Args, v, bits, a.calleeOf(fn))
	case *ssa.MakeClosure:
		lit := fn.Fn.(*ssa.Function)
		a.passed(instr, nil, common.Args, v, bits, a.calleeOf(lit))

		sub := a.literal(lit)

		for i, arg := range common.Args {
			if arg == v && !isGo {
				a.intoCells(fn, sub, i, bits)
			}
		}

		if call, ok := instr.(*ssa.Call); ok {
			for j, b := range fn.Bindings {
				if root := len(lit.Params) + j; root < maxRoots && sub.returned&(1<<uint(root)) != 0 {
					a.add(call, a.sets[b])
				}
			}
		}
	default:
		a.passed(instr, nil, common.Args, v, bits, callee{unknown: true})
	}
}

// calleeOf returns what fn does with its arguments: from its summary for a declared function
// whose arguments line up with its parameters, or else from its body, for a function literal
// or a synthetic wrapper, which has no summary of its own.
func (a *Analysis) calleeOf(fn *ssa.Function) callee {
	if obj, ok := fn.Object().(*types.Func); ok && arity(fn.Signature) == arity(obj.Signature()) {
		return callee{retains: a.lookup.Retains(obj), returns: a.lookup.Returns(obj), consumes: a.lookup.Consumes(obj)}
	}

	sub := a.literal(fn)

	return callee{retains: sub.Retained(), returns: sub.Returned()}
}

// passed follows v among args, the arguments of instr, into the callee c. A go statement keeps
// every argument, whatever the callee does. An argument packed into a variadic call's slice
// escapes as the values packed into it, each at its own store.
func (a *Analysis) passed(instr ssa.CallInstruction, fn *types.Func, args []ssa.Value, v ssa.Value, bits uint64, c callee) {
	_, isGo := instr.(*ssa.Go)
	result, hasResult := instr.(*ssa.Call)

	for i, arg := range args {
		if arg != v {
			continue
		}

		kind, escapes := PassedToKeeper, true

		switch {
		case isGo:
			kind = PassedToGoroutine
		case c.unknown:
			kind = PassedToUnknown
		case c.consumes.Has(i):
			kind = PassedToConsumer
		case !c.retains.Has(i):
			escapes = false
		}

		if escapes {
			for _, e := range a.elements(v, bits) {
				a.escape(instr, cmp.Or(e.at, ssa.Instruction(instr)), e.value, kind, fn, i, e.bits)
			}
		}

		if !c.unknown && c.returns.Has(i) && hasResult {
			a.derive(result, bits)
		}
	}
}

// elements returns what to report of v, carrying the roots in bits, where it escapes: v
// itself, or, when v is the slice a variadic call packs its arguments into, each argument
// that carries memory, at the store that packs it.
func (a *Analysis) elements(v ssa.Value, bits uint64) []kept {
	slice, ok := v.(*ssa.Slice)
	if !ok {
		return []kept{{at: nil, value: v, bits: bits}}
	}

	alloc, ok := slice.X.(*ssa.Alloc)
	if !ok || alloc.Comment != varargsComment {
		return []kept{{at: nil, value: v, bits: bits}}
	}

	var out []kept

	for _, r := range *alloc.Referrers() {
		if addr, ok := r.(*ssa.IndexAddr); ok {
			for _, r := range *addr.Referrers() {
				if st, ok := r.(*ssa.Store); ok && a.sets[st.Val]&bits != 0 {
					out = append(out, kept{at: st, value: st.Val, bits: a.sets[st.Val] & bits})
				}
			}
		}
	}

	return out
}

// builtin follows v into a builtin: append and copy put elements into a slice, and a few
// return memory of their argument; the others keep nothing.
func (a *Analysis) builtin(instr ssa.CallInstruction, fn *ssa.Builtin, v ssa.Value, bits uint64) {
	args := instr.Common().Args
	result, hasResult := instr.(*ssa.Call)

	switch fn.Name() {
	case builtinAppend:
		if args[0] == v && hasResult {
			a.add(result, bits)
		}

		if args[1] == v && elementShares(v.Type()) {
			if hasResult {
				a.add(result, bits)
			}

			for _, e := range a.elements(v, bits) {
				a.kept(instr, cmp.Or(e.at, ssa.Instruction(instr)), args[0], e.value, Appended, e.bits)
			}
		}
	case builtinCopy:
		if args[1] == v && elementShares(v.Type()) {
			a.kept(instr, instr, args[0], v, StoredIntoElement, bits)
		}
	case builtinWrapNilChk, builtinSlice, builtinSliceData, builtinString, builtinStringData, builtinAdd:
		if hasResult {
			a.derive(result, bits)
		}
	}
}

// baseOf returns the value a container is addressed from: the start of a chain of field,
// element, and slice operations. A load ends the chain: what it yields may be anyone's memory,
// whatever it was loaded from.
func baseOf(v ssa.Value) ssa.Value {
	for {
		switch x := v.(type) {
		case *ssa.FieldAddr:
			v = x.X
		case *ssa.IndexAddr:
			v = x.X
		case *ssa.Slice:
			v = x.X
		default:
			return v
		}
	}
}

// rootedInGlobal reports whether a container is addressed from a global, through any fields,
// elements, slices, and loads: what is stored into it is kept by the global.
func rootedInGlobal(v ssa.Value) bool {
	for {
		switch x := v.(type) {
		case *ssa.FieldAddr:
			v = x.X
		case *ssa.IndexAddr:
			v = x.X
		case *ssa.Slice:
			v = x.X
		case *ssa.UnOp:
			v = x.X
		case *ssa.Global:
			return true
		default:
			return false
		}
	}
}

// isError reports whether t is the predeclared error type.
func isError(t types.Type) bool {
	return types.Identical(t, types.Universe.Lookup("error").Type())
}

// elementShares reports whether the elements of a slice, or the bytes of a string, carry
// memory: what append and copy put into another slice.
func elementShares(t types.Type) bool {
	s, ok := t.Underlying().(*types.Slice)
	return ok && Shares(s.Elem())
}

// arity counts a signature's parameters, the receiver included.
func arity(sig *types.Signature) int {
	n := sig.Params().Len()
	if sig.Recv() != nil {
		n++
	}

	return n
}
