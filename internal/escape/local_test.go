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
	"testing"

	"github.com/tochemey/gomemsafe/internal/effect"
)

// localSrc stores parameters into memory the function made, into memory several parameters
// may share, and into variables captured by literals.
const localSrc = `package p

type T struct {
	next *T
	err  error
	n    int
}

type Holder struct {
	t     *T
	items []*T
}

var global *T

var sink error

func merged(c bool, h *T, t *T) {
	x := h
	if c {
		x = t
	}
	x.next = t
}

func nilAppend(t *T) int {
	var items []*T
	items = append(items, t)
	return len(items)
}

func nilAppendKept(h *Holder, t *T) {
	var items []*T
	items = append(items, t)
	h.items = items
}

func loopAppend(a, b *T, k int) int {
	var items []*T
	for i := 0; i < k; i++ {
		items = append(items, a)
		items = append(items, b)
	}
	return len(items)
}

func loopAppendForeign(h *Holder, t *T, k int) {
	items := h.items
	for i := 0; i < k; i++ {
		items = append(items, t)
	}
}

func madeSlice(t *T) int {
	items := make([]*T, 2)
	items[1:][0] = t
	return len(items)
}

func cellLocal(t *T) int {
	var n *T
	func() { n = t }()
	return n.n
}

func cellEscapes(t *T) {
	var n *T
	func() { n = t }()
	global = n
}

func cellParam(t *T) {
	var n *T
	func(x *T) { n = x }(t)
	global = n
}

func cellNested(t *T) {
	var n *T
	func() {
		func() { n = t }()
	}()
	global = n
}

func cellField(t *T) {
	var h Holder
	func() { h.t = t }()
	global = h.t
}

func errReturned(t *T) error { return t.err }

func errStored(t *T) { sink = t.err }

func errOfValue(t T) error { return t.err }

type pair struct {
	t   *T
	err error
}

func fieldOfValue(h *Holder, t *T) { h.t = pair{t: t}.t }

func errFieldOfValue(t *T) error { return pair{t: t}.err }
`

// TestOwnMemoryKeepsNothing checks which parameters escape when stored into memory the
// function made, into memory that may be one of several parameters, and into variables
// captured by literals.
func TestOwnMemoryKeepsNothing(t *testing.T) {
	pkg := buildEscapes(t, localSrc)

	for _, tc := range []struct {
		fn                 string
		retained, returned effect.ArgSet
	}{
		{"merged", 1 << 2, 0},
		{"nilAppend", 0, 0},
		{"nilAppendKept", 1 << 1, 0},
		{"loopAppend", 0, 0},
		{"loopAppendForeign", 1 << 1, 0},
		{"madeSlice", 0, 0},
		{"cellLocal", 0, 0},
		{"cellEscapes", 1 << 0, 0},
		{"cellParam", 1 << 0, 0},
		{"cellNested", 1 << 0, 0},
		{"cellField", 1 << 0, 0},
		{"errReturned", 0, 0},
		{"errStored", 0, 0},
		{"errOfValue", 0, 0},
		{"fieldOfValue", 1 << 1, 0},
		{"errFieldOfValue", 0, 0},
	} {
		a := Analyze(pkg.Func(tc.fn), fakeLookup{})
		if a.Retained() != tc.retained || a.Returned() != tc.returned {
			t.Errorf("%s: retained %#x, returned %#x; want %#x, %#x", tc.fn, uint64(a.Retained()), uint64(a.Returned()),
				uint64(tc.retained), uint64(tc.returned))
		}
	}
}

// TestMergedContainerEscapesWhereStored checks that the value stored into memory that may be
// another parameter's is reported at the store.
func TestMergedContainerEscapesWhereStored(t *testing.T) {
	escapes := Analyze(buildEscapes(t, localSrc).Func("merged"), fakeLookup{}).Escapes(2)
	if len(escapes) != 1 || escapes[0].Kind != StoredIntoField {
		t.Fatalf("escapes %+v, want one store into a field", escapes)
	}
}
