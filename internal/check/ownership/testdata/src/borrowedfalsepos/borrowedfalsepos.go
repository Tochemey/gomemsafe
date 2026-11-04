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

// Package borrowedfalsepos holds look-alike correct code: borrowed parameters that are read,
// written into, copied, kept only within the call, or passed to callees that keep nothing.
// MS502 must stay silent.
package borrowedfalsepos

import (
	"borrowlib"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// T is a linked node.
type T struct {
	next *T
	n    int
}

// Holder keeps nodes.
type Holder struct {
	t     *T
	items []*T
	buf   []byte
	n     int
}

var global *T

func use(...any) {}

// Reads reads t.
//
//memsafe:borrows t
func Reads(t *T) int { return t.n }

// Copies copies a number out of t.
//
//memsafe:borrows t
func (h *Holder) Copies(t *T) { h.n = t.n }

// Lent passes t to a callee that borrows it.
//
//memsafe:borrows t
func Lent(t *borrowlib.Node) { borrowlib.Lend(t) }

// Inspected passes t to a callee whose fact says it keeps nothing.
//
//memsafe:borrows t
func Inspected(t *borrowlib.Node) int { return borrowlib.Inspect(t) }

// Printed passes t to a printer known to keep nothing.
//
//memsafe:borrows t
func Printed(t *T) { fmt.Println(t) }

// Written passes b to an io.Writer, which must not retain it.
//
//memsafe:borrows b
func Written(w io.Writer, b []byte) { _, _ = w.Write(b) }

// LocalOnly keeps t in a node that stays in the call.
//
//memsafe:borrows t
func LocalOnly(t *T) int {
	x := &T{next: t}
	return x.n
}

// InLiteral reads t from a literal.
//
//memsafe:borrows t
func InLiteral(t *T) { func() { use(t.n) }() }

// SelfStored stores t into its own memory, which reaches nothing new.
//
//memsafe:borrows t
func SelfStored(t *T) { t.next = t }

// LocalChan sends t on a channel that stays in the call.
//
//memsafe:borrows t
func LocalChan(t *T) {
	ch := make(chan *T, 1)
	ch <- t
	<-ch
}

// Overwritten writes into t.
//
//memsafe:borrows t
func Overwritten(t *T) { *t = T{} }

// Compared compares t.
//
//memsafe:borrows t
func Compared(t *T) bool { return t == global }

// Copied copies the bytes of b, which carry no memory.
//
//memsafe:borrows b
func (h *Holder) Copied(b []byte) { h.buf = append(h.buf[:0], b...) }

// Deferred passes t to a callee that keeps nothing, at the return.
//
//memsafe:borrows t
func Deferred(t *borrowlib.Node) { defer borrowlib.Inspect(t) }

// OwnItems stores h's own memory into h.
//
//memsafe:borrows h
func (h *Holder) OwnItems() { h.items = append(h.items, h.t) }

// Unborrowed keeps t, but borrows nothing: MS502 has nothing to check.
func (h *Holder) Unborrowed(t *T) { h.t = t }

// Collected appends t to a slice the function made, and keeps only its length.
//
//memsafe:borrows t
func Collected(t *T) int {
	var items []*T
	items = append(items, t)

	return len(items)
}

// CollectedInLoop appends two borrowed values to its own slice in a loop.
//
//memsafe:borrows a
//memsafe:borrows b
func CollectedInLoop(a, b *T, k int) int {
	var items []*T

	for range k {
		items = append(items, a, b)
	}

	return len(items)
}

// CapturedLocally assigns t to a variable inside a literal, and only reads it.
//
//memsafe:borrows t
func CapturedLocally(t *T) int {
	var n *T

	func() { n = t }()

	return n.n
}

// Flushed returns the error the writer kept, which is a value of its own.
//
//memsafe:borrows w
func Flushed(w *bufio.Writer) error { return w.Flush() }

// Waited waits on the context it borrows.
//
//memsafe:borrows ctx
func Waited(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

// Encoded encodes v, which the encoder does not keep.
//
//memsafe:borrows v
func Encoded(w io.Writer, v *T) error { return json.NewEncoder(w).Encode(v) }
