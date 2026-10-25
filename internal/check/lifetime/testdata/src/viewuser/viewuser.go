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

// Package viewuser uses views that directives and package viewlib's summaries declare, after a
// call ended them. MS302 must report each one, and stay silent on the look-alike correct code.
package viewuser

import (
	"bufio"
	"bytes"
	"viewlib"
)

// ring declares its views in this package.
type ring struct{ b []byte }

func use([]byte) {}

// peek returns a view of r.
//
//memsafe:viewof r
func (r *ring) peek() []byte { return r.b }

// advance moves past the first byte.
//
//memsafe:invalidates r
func (r *ring) advance() { r.b = r.b[1:] }

func annotated(b *viewlib.Buffer) {
	v := b.Bytes()
	b.Reset()
	use(v) // want `MS302: v used after b.Reset ended the views into b`
}

func annotatedHere(r *ring) {
	v := r.peek()
	r.advance()
	use(v) // want `MS302: v used after r.advance ended the views into r`
}

func annotatedRows(t *viewlib.Table) string {
	rows := t.Rows()
	t.Clear()

	return rows[0] // want `MS302: rows used after t.Clear ended the views into t`
}

func helperView(buf *bytes.Buffer) {
	v := viewlib.Contents(buf)
	buf.Reset()
	use(v) // want `MS302: v used after buf.Reset ended the views into buf`
}

func helperEnds(buf *bytes.Buffer) {
	v := buf.Bytes()
	viewlib.Clear(buf)
	use(v) // want `MS302: v used after viewlib.Clear ended the views into buf`
}

func helperBoth(buf *bytes.Buffer) {
	v := viewlib.Contents(buf)
	viewlib.Fill(0, buf)
	use(v) // want `MS302: v used after viewlib.Fill ended the views into buf`
}

func secondArgument(a, b *bytes.Buffer) {
	v := viewlib.Second(a, b)
	b.Reset()
	use(v) // want `MS302: v used after b.Reset ended the views into b`
}

func emitted(w *bufio.Writer) {
	v := w.AvailableBuffer()
	viewlib.Emit(w, "x")
	use(v) // want `MS302: v used after viewlib.Emit ended the views into w`
}

// recycled: Recycle's body only truncates, which no AvailableBuffer view minds, but its
// directive ends every view.
func recycled(buf *bytes.Buffer) {
	v := buf.AvailableBuffer()
	viewlib.Recycle(buf)
	use(v) // want `MS302: v used after viewlib.Recycle ended the views into buf`
}

// written: Buffer.Write declares no contract, so the view is still valid.
func written(b *viewlib.Buffer) {
	v := b.Bytes()
	b.Write([]byte("x"))
	use(v)
}

func otherBuffer(a, b *viewlib.Buffer) {
	v := a.Bytes()
	b.Reset()
	use(v)
}

func secondArgumentKept(a, b *bytes.Buffer) {
	v := viewlib.Second(a, b)
	a.Reset()
	use(v)
}

func maybeCleared(c bool, buf *bytes.Buffer) {
	v := buf.Bytes()
	viewlib.MaybeClear(c, buf)
	use(v)
}

func copied(buf *bytes.Buffer) {
	v := viewlib.Copy(buf)
	buf.Reset()
	use(v)
}

// readAfterFill: Fill writes into buf, which leaves r's views valid.
func readAfterFill(buf *bytes.Buffer, r *bufio.Reader) {
	v, _ := r.Peek(1)
	viewlib.Fill(0, buf)
	use(v)
}

// connReset: the views and the clear are of a field of c, which the summaries do not follow.
func connReset(c *viewlib.Conn) {
	v := c.Bytes()
	c.Reset()
	use(v)
}

func usedBefore(b *viewlib.Buffer) {
	v := b.Bytes()
	use(v)
	b.Reset()
}

func lengthOnly(b *viewlib.Buffer) int {
	v := b.Bytes()
	b.Reset()

	return len(v)
}

func replaced(b *viewlib.Buffer) {
	v := b.Bytes()
	*b = viewlib.Buffer{}
	b.Reset()
	use(v)
}
