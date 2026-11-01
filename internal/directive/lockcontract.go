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

package directive

import "slices"

// LockRefKind says where a function's contract finds a lock.
type LockRefKind int

// Guard is one lock that a +checklocks annotation names as guarding a field or a global
// variable. It holds no positions and no pointers, so that it can be exported in a fact.
type Guard struct {
	// Name is the lock as the annotation writes it, for diagnostics.
	Name string
	// Global is the global lock's key, its package path and name joined by a dot, or "" for a
	// lock that Path reaches from the struct declaring the guarded field.
	Global string
	// Path lists the field indexes from the struct, or from the global, to the lock. Each step
	// through a pointer field dereferences it.
	Path []int
}

// LockRef is a lock that a function's contract names.
type LockRef struct {
	Name string
	Kind LockRefKind
	// Index is the call argument, the receiver first, or the result that holds the lock.
	Index int
	// Global is the global lock's key, for LockGlobal.
	Global string
	// Path lists the field indexes from the argument, result, or global to the lock.
	Path []int
	// Exclusive says the lock is held, acquired, or released exclusively rather than for
	// reading. An excluded lock must not be held in any mode.
	Exclusive bool
}

// LockContract is what the checklocks annotations in a function's doc comment declare.
type LockContract struct {
	// Requires lists the locks held on entry and still held on exit: +checklocks and
	// +checklocksread.
	Requires []LockRef
	// Acquires lists the locks not held on entry and held on exit.
	Acquires []LockRef
	// Releases lists the locks held on entry and not held on exit.
	Releases []LockRef
	// Excludes lists the locks that must not be held on entry.
	Excludes []LockRef
	// Ignore says the body is not checked, as +checklocksignore asks; the contract still binds
	// callers.
	Ignore bool
}

// Empty reports whether the contract declares nothing callers or the body must honour.
func (c *LockContract) Empty() bool {
	return c == nil || !c.Ignore && len(c.Requires) == 0 && len(c.Acquires) == 0 &&
		len(c.Releases) == 0 && len(c.Excludes) == 0
}

// Equal reports whether two contracts declare the same, so that a summary fact changes only
// when a contract does.
func (c *LockContract) Equal(o *LockContract) bool {
	if c == nil || o == nil {
		return c.Empty() && o.Empty()
	}

	return c.Ignore == o.Ignore && refsEqual(c.Requires, o.Requires) && refsEqual(c.Acquires, o.Acquires) &&
		refsEqual(c.Releases, o.Releases) && refsEqual(c.Excludes, o.Excludes)
}

// HeldOnEntry returns the locks a caller must hold: the required and the released ones.
func (c *LockContract) HeldOnEntry() []LockRef {
	return append(append([]LockRef(nil), c.Requires...), c.Releases...)
}

// HeldOnExit returns the locks a call leaves held: the required and the acquired ones.
func (c *LockContract) HeldOnExit() []LockRef {
	return append(append([]LockRef(nil), c.Requires...), c.Acquires...)
}

func refsEqual(a, b []LockRef) bool {
	return slices.EqualFunc(a, b, func(x, y LockRef) bool {
		return x.Name == y.Name && x.Kind == y.Kind && x.Index == y.Index && x.Global == y.Global &&
			x.Exclusive == y.Exclusive && slices.Equal(x.Path, y.Path)
	})
}
