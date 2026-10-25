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

// Package viewlib returns views and ends them for package viewuser: through directives on types
// of its own, and through helpers that wrap the standard library's views, which its summaries
// carry.
package viewlib

import (
	"bufio"
	"bytes"
)

// Buffer is a byte buffer whose views stay valid until the next Reset.
type Buffer struct{ b []byte }

// Bytes returns a view that is valid until the next Reset.
//
//memsafe:viewof b
func (b *Buffer) Bytes() []byte { return b.b }

// Reset empties the buffer.
//
//memsafe:invalidates b
func (b *Buffer) Reset() { b.b = b.b[:0] }

// Write appends p. It declares no contract, so it ends no view.
func (b *Buffer) Write(p []byte) { b.b = append(b.b, p...) }

// Table holds rows, and its views are slices of them.
type Table struct{ rows []string }

// Rows returns a view of t's rows.
//
//memsafe:viewof t
func (t *Table) Rows() []string { return t.rows }

// Clear drops every row.
//
//memsafe:invalidates t
func (t *Table) Clear() { t.rows = t.rows[:0] }

// Conn holds its buffer by value; its methods make views of that field and end them.
type Conn struct{ buf bytes.Buffer }

// Bytes returns a view of c's buffer.
func (c *Conn) Bytes() []byte { return c.buf.Bytes() }

// Reset clears c's buffer.
func (c *Conn) Reset() { c.buf.Reset() }

// Contents returns the view bytes.Buffer.Bytes makes.
func Contents(buf *bytes.Buffer) []byte { return buf.Bytes() }

// Second returns a view into its second argument.
func Second(_, b *bytes.Buffer) []byte { return b.Bytes() }

// Copy returns a copy of buf's contents.
func Copy(buf *bytes.Buffer) []byte { return bytes.Clone(buf.Bytes()) }

// Clear clears buf.
func Clear(buf *bytes.Buffer) { buf.Reset() }

// MaybeClear clears buf on some paths only.
func MaybeClear(c bool, buf *bytes.Buffer) {
	if c {
		buf.Reset()
	}
}

// Fill writes into its second argument.
func Fill(_ int, buf *bytes.Buffer) { buf.WriteByte('x') }

// Emit writes into w.
func Emit(w *bufio.Writer, s string) { _, _ = w.WriteString(s) }

// Recycle ends every view of buf, more than its body shows.
//
//memsafe:invalidates buf
func Recycle(buf *bytes.Buffer) { buf.Truncate(0) }
