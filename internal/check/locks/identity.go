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

package locks

import (
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/directive"
	"github.com/tochemey/gomemsafe/internal/flow"
)

// The methods that change a lock's state, on a Mutex, an RWMutex, or a sync.Locker. The
// nested and downgrade forms are gVisor's own sync package's.
const (
	// constPrefix starts the key of a constant, which is no global.
	constPrefix = "const "

	methodLock          = "Lock"
	methodRLock         = "RLock"
	methodUnlock        = "Unlock"
	methodRUnlock       = "RUnlock"
	methodNestedLock    = "NestedLock"
	methodNestedUnlock  = "NestedUnlock"
	methodDowngradeLock = "DowngradeLock"
)

// key identifies a lock, or a value a lock is reached from: a root value, or a global, and a
// path from it of field indexes and element indexes. Two accesses with equal keys reach the
// same memory, as far as the analysis follows: it does not follow a field a function stores
// into between them. A constant is its own key, whichever instruction holds it.
type key struct {
	root   ssa.Value
	global string
	// path is the steps joined by dots: a field's index, or an element's index key in
	// brackets, so that a key is comparable.
	path string
}

func (k key) field(i int) key {
	if k.path == "" {
		k.path = strconv.Itoa(i)
	} else {
		k.path += "." + strconv.Itoa(i)
	}

	return k
}

func (k key) extend(path []int) key {
	for _, i := range path {
		k = k.field(i)
	}

	return k
}

// steps returns the steps of the path: a field index, or an index key's number in brackets.
func (k key) steps() []string {
	if k.path == "" {
		return nil
	}

	return strings.Split(k.path, ".")
}

// resolve returns the key of v in frame f: through fields, conversions, the bindings of
// inlined literals, and loads. A load of a tracked local variable yields the value the variable
// held at the load, when state says which, and otherwise the variable itself, so that every
// load of a variable whose contents are not known is one lock, as checklocks keys it; a load
// through any other address keeps the address's key, since the analysis does not follow stores
// into fields. seen stops a chain of loads that lead back to themselves.
func (u *universe) resolve(v ssa.Value, f *frame, state *flow.Bitset, seen map[*ssa.UnOp]bool) key {
	switch x := v.(type) {
	case *ssa.FieldAddr:
		return u.resolve(x.X, f, state, seen).field(x.Field)
	case *ssa.Field:
		return u.resolve(x.X, f, state, seen).field(x.Field)
	case *ssa.Convert:
		return u.resolve(x.X, f, state, seen)
	case *ssa.ChangeType:
		return u.resolve(x.X, f, state, seen)
	case *ssa.IndexAddr:
		return u.resolve(x.X, f, state, seen).index(u.indexID(u.resolve(x.Index, f, state, seen)))
	case *ssa.Index:
		return u.resolve(x.X, f, state, seen).index(u.indexID(u.resolve(x.Index, f, state, seen)))
	case *ssa.Const:
		return constKey(x)
	case *ssa.Global:
		return key{global: directive.GlobalKey(x.Object().(*types.Var))}
	case *ssa.Parameter, *ssa.FreeVar:
		if f != nil {
			if bound, ok := f.bindings[v]; ok {
				return u.resolve(bound, f.parent, state, seen)
			}
		}
	case *ssa.UnOp:
		if x.Op != token.MUL {
			break
		}

		base := u.resolve(x.X, f, state, seen)

		alloc, tracked := u.isAlloc(base)
		if !tracked {
			return base
		}

		if state == nil || seen[x] {
			return base
		}

		if seen == nil {
			seen = make(map[*ssa.UnOp]bool)
		}

		seen[x] = true

		for _, s := range u.stores[alloc] {
			if i, ok := u.is[isPair{x, s}]; ok && state.Has(i) {
				return u.resolve(s.v, s.f, state, seen)
			}
		}

		return base
	}

	return key{root: v}
}

// possible returns every key v may resolve to, in any state: for a load of a tracked local
// variable, the keys of every value stored into it, and the variable itself.
func (u *universe) possible(v ssa.Value, f *frame, seen map[*ssa.UnOp]bool) []key {
	extend := func(base ssa.Value, field int) []key {
		keys := u.possible(base, f, seen)
		for i := range keys {
			keys[i] = keys[i].field(field)
		}

		return keys
	}

	indexed := func(base, index ssa.Value) []key {
		var out []key

		for _, b := range u.possible(base, f, seen) {
			for _, i := range u.possible(index, f, seen) {
				out = append(out, b.index(u.indexID(i)))
			}
		}

		return out
	}

	switch x := v.(type) {
	case *ssa.FieldAddr:
		return extend(x.X, x.Field)
	case *ssa.Field:
		return extend(x.X, x.Field)
	case *ssa.Convert:
		return u.possible(x.X, f, seen)
	case *ssa.ChangeType:
		return u.possible(x.X, f, seen)
	case *ssa.IndexAddr:
		return indexed(x.X, x.Index)
	case *ssa.Index:
		return indexed(x.X, x.Index)
	case *ssa.Const:
		return []key{constKey(x)}
	case *ssa.Global:
		return []key{{global: directive.GlobalKey(x.Object().(*types.Var))}}
	case *ssa.Parameter, *ssa.FreeVar:
		if f != nil {
			if bound, ok := f.bindings[v]; ok {
				return u.possible(bound, f.parent, seen)
			}
		}
	case *ssa.UnOp:
		if x.Op != token.MUL {
			break
		}

		bases := u.possible(x.X, f, seen)
		out := make([]key, 0, len(bases))

		for _, base := range bases {
			alloc, tracked := u.isAlloc(base)
			if !tracked {
				out = append(out, base)
				continue
			}

			if seen == nil {
				seen = make(map[*ssa.UnOp]bool)
			}

			if !seen[x] {
				seen[x] = true

				for _, s := range u.stores[alloc] {
					out = append(out, u.possible(s.v, s.f, seen)...)
				}
			}

			out = append(out, base)
		}

		return out
	}

	return []key{{root: v}}
}

// constKey returns the key of a constant: its value, so that two occurrences of the same
// constant index the same element.
func constKey(c *ssa.Const) key {
	if c.Value == nil {
		return key{global: constPrefix + "nil"}
	}

	return key{global: constPrefix + c.Value.ExactString()}
}

// guardKey returns the key of the lock guard names, for an access whose struct is base.
func guardKey(guard directive.Guard, base key) key {
	if guard.Global != "" {
		return key{global: guard.Global}.extend(guard.Path)
	}

	return base.extend(guard.Path)
}

// resultOf returns the result at index of the call value v: v itself for a single result, or
// the extraction of that element of its tuple, if the call extracts it. For the results of a
// return, it is the result itself.
func resultOf(v ssa.Value, index int) (ssa.Value, bool) {
	if v == nil {
		return nil, false
	}

	if t, isReturn := v.(returnTuple); isReturn {
		if index >= len(t.ret.Results) {
			return nil, false
		}

		return t.ret.Results[index], true
	}

	if _, isTuple := v.Type().(*types.Tuple); !isTuple {
		return v, index == 0
	}

	for _, ref := range *v.Referrers() {
		if x, ok := ref.(*ssa.Extract); ok && x.Index == index {
			return x, true
		}
	}

	return nil, false
}

// refValue returns the value the parameter or result reference ref names: with no args, the
// parameter of f's function; at a call, the argument, or the result the call extracts. A
// result no call extracts, or that a deferred call has not yet, names no value.
func refValue(ref directive.LockRef, args []ssa.Value, result ssa.Value, f *frame) (ssa.Value, bool) {
	switch {
	case ref.Kind == directive.LockResult:
		return resultOf(result, ref.Index)
	case args == nil:
		return f.fn.Params[ref.Index], true
	}

	return args[ref.Index], true
}

// lockMethod reports whether common calls a method that changes a lock's state: Lock, RLock,
// Unlock, RUnlock, or gVisor's nested and downgrade forms, on a Mutex, an RWMutex, or through a
// sync.Locker. It returns the lock and the method's name.
func lockMethod(common *ssa.CallCommon) (ssa.Value, string, bool) {
	var (
		name     string
		receiver ssa.Value
	)

	switch {
	case common.IsInvoke():
		name, receiver = common.Method.Name(), common.Value
	default:
		callee, ok := common.Value.(*ssa.Function)
		if !ok || callee.Signature.Recv() == nil || len(common.Args) == 0 {
			return nil, "", false
		}

		name, receiver = callee.Name(), common.Args[0]
	}

	switch name {
	case methodLock, methodRLock, methodUnlock, methodRUnlock, methodNestedLock, methodNestedUnlock, methodDowngradeLock:
	default:
		return nil, "", false
	}

	if _, isLock := directive.LockKind(receiver.Type()); !isLock {
		return nil, "", false
	}

	return receiver, name, true
}

// fresh reports whether v is, or is part of, a value the function allocated itself: checklocks
// does not check the guards of a fresh value, since nothing else can reach it yet.
func fresh(v ssa.Value) bool {
	switch x := v.(type) {
	case *ssa.Alloc:
		return true
	case *ssa.FieldAddr:
		return fresh(x.X)
	case *ssa.Field:
		return fresh(x.X)
	case *ssa.IndexAddr:
		return fresh(x.X)
	case *ssa.Index:
		return fresh(x.X)
	case *ssa.Convert:
		return fresh(x.X)
	case *ssa.ChangeType:
		return fresh(x.X)
	}

	return false
}

// isWrite reports whether the address v is stored through. An address passed on may be written
// too; as checklocks, the analysis takes it as read.
func isWrite(v ssa.Value) bool {
	for _, ref := range *v.Referrers() {
		if s, ok := ref.(*ssa.Store); ok && s.Addr == v {
			return true
		}
	}

	return false
}

// fieldOf returns the field at index of the struct t is, or points to.
func fieldOf(t types.Type, index int) (*types.Var, bool) {
	st, ok := directive.StructOf(t)
	if !ok || index >= st.NumFields() {
		return nil, false
	}

	return st.Field(index), true
}
