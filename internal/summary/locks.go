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
	"sort"
	"strings"

	"github.com/tochemey/gomemsafe/internal/directive"
)

// GuardFact marks a field or a global variable that checklocks annotations guard, or name as
// a lock. Only such objects get one, so at least one part is set.
type GuardFact struct {
	// Guards lists the locks that guard the object.
	Guards []directive.Guard
	// Lock says some annotation names the object as a lock.
	Lock bool
}

// AFact implements analysis.Fact.
func (*GuardFact) AFact() {}

// String lists the guards by name, guards(mu, other.mu), and "lock" for a lock.
func (f *GuardFact) String() string {
	var parts []string

	if len(f.Guards) > 0 {
		names := make([]string, len(f.Guards))
		for i, g := range f.Guards {
			names[i] = g.Name
		}

		parts = append(parts, "guards("+strings.Join(names, ", ")+")")
	}

	if f.Lock {
		parts = append(parts, "lock")
	}

	return strings.Join(parts, " ")
}

// GuardsOf returns the locks that guard obj, a field or a global, declared in any package.
func (r *Result) GuardsOf(obj types.Object) []directive.Guard { return r.guardFactOf(obj).Guards }

// IsLock reports whether some annotation, in any package, names obj as a lock.
func (r *Result) IsLock(obj types.Object) bool { return r.guardFactOf(obj).Lock }

// LockContractOf returns the lock contract of fn, declared in any package.
func (r *Result) LockContractOf(fn *types.Func) (*directive.LockContract, bool) {
	if fn.Origin().Pkg() == r.pass.Pkg {
		return r.dir.LockContractOf(fn)
	}

	c := r.factOf(fn).Locks

	return c, !c.Empty()
}

// HasLockFacts reports whether obj takes part in lock checking: it is guarded, it is a lock, or
// it is a function with a lock contract.
func (r *Result) HasLockFacts(obj types.Object) bool {
	if fn, ok := obj.(*types.Func); ok {
		_, has := r.LockContractOf(fn)
		return has
	}

	f := r.guardFactOf(obj)

	return f.Lock || len(f.Guards) > 0
}

// guardFactOf returns the guard fact of obj: from this package's annotations, and, for an
// object of another package, from the fact that package exported. An annotation of this
// package may name a lock of another, through an embedded field, so both are consulted. A
// field of an instantiated generic type has the fact of the field it is instantiated from.
func (r *Result) guardFactOf(obj types.Object) GuardFact {
	if v, ok := obj.(*types.Var); ok {
		obj = v.Origin()
	}

	f := GuardFact{Guards: r.dir.GuardsOf(obj), Lock: r.dir.IsLock(obj)}
	if obj.Pkg() == r.pass.Pkg || obj.Pkg() == nil {
		return f
	}

	imported, ok := r.guardImported[obj]
	if !ok {
		r.pass.ImportObjectFact(obj, &imported)
		r.guardImported[obj] = imported
	}

	return GuardFact{Guards: imported.Guards, Lock: f.Lock || imported.Lock}
}

// exportGuards exports a GuardFact for every guarded object and every lock of this package, in
// position order, so that the facts file does not depend on how the annotations were found. A
// lock of another package that an annotation names gets no fact: facts belong to the package
// that declares the object.
func (r *Result) exportGuards() {
	guarded, locks, _ := r.dir.LockFacts()

	objects := make(map[types.Object]bool)

	for _, obj := range append(append([]types.Object(nil), guarded...), locks...) {
		if obj.Pkg() == r.pass.Pkg {
			objects[obj] = true
		}
	}

	ordered := make([]types.Object, 0, len(objects))
	for obj := range objects {
		ordered = append(ordered, obj)
	}

	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Pos() < ordered[j].Pos() })

	for _, obj := range ordered {
		r.pass.ExportObjectFact(obj, &GuardFact{Guards: r.dir.GuardsOf(obj), Lock: r.dir.IsLock(obj)})
	}
}

// lockString words a contract: locks(requires: g.mu; acquires: g.mu; ignore).
func lockString(c *directive.LockContract) string {
	var parts []string

	for _, set := range []struct {
		name string
		refs []directive.LockRef
	}{{"requires", c.Requires}, {"acquires", c.Acquires}, {"releases", c.Releases}, {"excludes", c.Excludes}} {
		if len(set.refs) == 0 {
			continue
		}

		names := make([]string, len(set.refs))

		for i, ref := range set.refs {
			names[i] = ref.Name
			if !ref.Exclusive && set.name != "excludes" {
				names[i] += " (read)"
			}
		}

		parts = append(parts, set.name+": "+strings.Join(names, ", "))
	}

	if c.Ignore {
		parts = append(parts, "ignore")
	}

	return "locks(" + strings.Join(parts, "; ") + ")"
}
