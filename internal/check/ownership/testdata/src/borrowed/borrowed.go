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

// Package borrowed lets borrowed parameters escape in every way MS502 must report.
package borrowed

import "borrowlib"

// T is a linked node.
type T struct {
	next   *T
	holder *Holder
	buf    []byte
	n      int
}

// Holder keeps nodes.
type Holder struct {
	t     *T
	items []*T
	byID  map[int]*T
	ch    chan *T
	f     func()
	buf   []byte
	node  *borrowlib.Node
}

var global *T

func use(...any) {}

// Field stores t.
//
//memsafe:borrows t
func (h *Holder) Field(t *T) { h.t = t } // want `MS502: t escapes Field, which borrows it: stored into a field`

// Element stores t.
//
//memsafe:borrows t
func (h *Holder) Element(t *T) { h.items[0] = t } // want `MS502: t escapes Element, which borrows it: stored into an element`

// Entry stores t.
//
//memsafe:borrows t
func (h *Holder) Entry(t *T) { h.byID[1] = t } // want `MS502: t escapes Entry, which borrows it: stored into a map`

// Global stores t.
//
//memsafe:borrows t
func Global(t *T) { global = t } // want `MS502: t escapes Global, which borrows it: stored into a global`

// Pointer stores t.
//
//memsafe:borrows t
func Pointer(pp **T, t *T) { *pp = t } // want `MS502: t escapes Pointer, which borrows it: stored through a pointer`

// Appended keeps t in h: two escapes on one line, reported once, for t itself.
//
//memsafe:borrows t
func (h *Holder) Appended(t *T) { h.items = append(h.items, t) } // want `MS502: t escapes Appended, which borrows it: appended to a slice`

// Grown appends t to a slice it did not make.
//
//memsafe:borrows t
func Grown(items []*T, t *T) { _ = append(items, t) } // want `MS502: t escapes Grown, which borrows it: appended to a slice`

// Bytes keeps the bytes of b.
//
//memsafe:borrows b
func (h *Holder) Bytes(b []byte) { h.buf = b } // want `MS502: b escapes Bytes, which borrows it: stored into a field`

// Sent sends t.
//
//memsafe:borrows t
func (h *Holder) Sent(t *T) { h.ch <- t } // want `MS502: t escapes Sent, which borrows it: sent on a channel`

// Selected sends t in a select.
//
//memsafe:borrows t
func Selected(ch chan *T, t *T) {
	select {
	case ch <- t: // want `MS502: t escapes Selected, which borrows it: sent on a channel`
	default:
	}
}

// Returned returns t.
//
//memsafe:borrows t
func Returned(t *T) *T { return t } // want `MS502: t escapes Returned, which borrows it: returned`

// ReturnedNext returns memory reachable from t.
//
//memsafe:borrows t
func ReturnedNext(t *T) *T { return t.next } // want `MS502: t.next escapes ReturnedNext, which borrows t: returned`

// Started hands t to a goroutine.
//
//memsafe:borrows t
func Started(t *T) { go use(t) } // want `MS502: t escapes Started, which borrows it: passed to a goroutine`

// Captured starts a literal holding t.
//
//memsafe:borrows t
func Captured(t *T) {
	go func() { use(t) }() // want `MS502: t escapes Captured, which borrows it: captured by a goroutine`
}

// Kept passes t to a function whose fact says it keeps it.
//
//memsafe:borrows t
func Kept(t *borrowlib.Node) { borrowlib.Keep(t) } // want `MS502: t escapes Kept, which borrows it: passed to borrowlib.Keep, which keeps it`

// Consumed passes t to a consuming parameter.
//
//memsafe:borrows t
func Consumed(t *borrowlib.Node) { borrowlib.Take(t) } // want `MS502: t escapes Consumed, which borrows it: passed to the consuming parameter n of borrowlib.Take`

// Unknown passes t to an interface method.
//
//memsafe:borrows t
func Unknown(s borrowlib.Sink, t *borrowlib.Node) { s.Put(t) } // want `MS502: t escapes Unknown, which borrows it: passed to s.Put, which may keep it`

// Dynamic passes t to a function value.
//
//memsafe:borrows t
func Dynamic(f func(*T), t *T) { f(t) } // want `MS502: t escapes Dynamic, which borrows it: passed to f, which may keep it`

// Panicked panics with t.
//
//memsafe:borrows t
func Panicked(t *T) { panic(t) } // want `MS502: t escapes Panicked, which borrows it: passed to panic`

// ThroughLocal stores a node holding t.
//
//memsafe:borrows t
func (h *Holder) ThroughLocal(t *T) {
	x := &T{}
	x.next = t
	h.t = x // want `MS502: x escapes ThroughLocal, which borrows t: stored into a field`
}

// ThroughCall stores what a callee returns of t, which its fact says shares t's memory.
//
//memsafe:borrows t
func (h *Holder) ThroughCall(t *borrowlib.Node) {
	h.node = borrowlib.Head(t) // want `MS502: borrowlib.Head\(t\) escapes ThroughCall, which borrows t: stored into a field`
}

// InLiteral stores t from a literal called directly.
//
//memsafe:borrows t
func (h *Holder) InLiteral(t *T) {
	func() { h.t = t }() // want `MS502: t escapes InLiteral, which borrows it: stored into a field`
}

// Deferred stores t from a deferred literal.
//
//memsafe:borrows t
func (h *Holder) Deferred(t *T) {
	defer func() { h.t = t }() // want `MS502: t escapes Deferred, which borrows it: stored into a field`
}

// Stored keeps a literal holding t.
//
//memsafe:borrows t
func (h *Holder) Stored(t *T) {
	h.f = func() { use(t) } // want `MS502: a function literal capturing t escapes Stored, which borrows t: stored into a field`
}

// Self stores its receiver into t.
//
//memsafe:borrows h
func (h *Holder) Self(t *T) { t.holder = h } // want `MS502: h escapes Self, which borrows it: stored into a field`

// Two borrows two parameters and keeps both.
//
//memsafe:borrows a
//memsafe:borrows b
func (h *Holder) Two(a, b *T) {
	h.t = a        // want `MS502: a escapes Two, which borrows it: stored into a field`
	h.items[0] = b // want `MS502: b escapes Two, which borrows it: stored into an element`
}

// Twice keeps t twice on one line, which is one report.
//
//memsafe:borrows t
func (h *Holder) Twice(t *T) { h.t, global = t, t } // want `MS502: t escapes Twice, which borrows it: stored into a field`

// Merged keeps what may be t.
//
//memsafe:borrows t
func (h *Holder) Merged(c bool, t *T) {
	x := &T{}
	if c {
		x = t
	}

	h.t = x // want `MS502: x escapes Merged, which borrows t: stored into a field`
}

// Ranged keeps an element of items.
//
//memsafe:borrows items
func (h *Holder) Ranged(items []*T) {
	for _, t := range items {
		h.t = t // want `MS502: t escapes Ranged, which borrows items: stored into a field`
	}
}

// MergedInto stores t into what may be h: t escapes on the path where it is.
//
//memsafe:borrows t
func MergedInto(c bool, h, t *T) {
	x := h
	if c {
		x = t
	}

	x.next = t // want `MS502: t escapes MergedInto, which borrows it: stored into a field`
}

// MapKey keeps t as a key of a map it did not make.
//
//memsafe:borrows t
func MapKey(m map[*T]int, t *T) { m[t] = 1 } // want `MS502: t escapes MapKey, which borrows it: stored into a map`

// ViaHelper passes t to a helper that keeps it as a map key; the helper's summary must say so.
//
//memsafe:borrows t
func ViaHelper(m map[*T]int, t *T) { index(m, t) } // want `MS502: t escapes ViaHelper, which borrows it: passed to index, which keeps it`

func index(m map[*T]int, t *T) { m[t] = 1 }

// ViaDeclared passes t to a helper that keeps it through a variable declaration.
//
//memsafe:borrows t
func ViaDeclared(t *T) { remember(t) } // want `MS502: t escapes ViaDeclared, which borrows it: passed to remember, which keeps it`

func remember(t *T) {
	var kept = t
	global = kept
}

// CapturedThenKept assigns t to a variable inside a literal, then keeps the variable.
//
//memsafe:borrows t
func CapturedThenKept(t *T) {
	var n *T

	func() { n = t }()

	global = n // want `MS502: n escapes CapturedThenKept, which borrows t: stored into a global`
}

// Wide captures t in a literal with more parameters and captures than one analysis follows.
//
//memsafe:borrows t
func Wide(t *T) {
	_ = func(a0, a1, a2, a3, a4, a5, a6, a7, a8, a9, a10, a11, a12, a13, a14, a15, a16, a17, a18, a19, a20, a21, a22, a23, a24, a25, a26, a27, a28, a29, a30, a31, a32, a33, a34, a35, a36, a37, a38, a39, a40, a41, a42, a43, a44, a45, a46, a47, a48, a49, a50, a51, a52, a53, a54, a55, a56, a57, a58, a59, a60, a61, a62, a63 int) { // want `MS502: t escapes Wide, which borrows it: captured by a function literal with too many parameters and captures to analyse`
		use(t)
	}
}
