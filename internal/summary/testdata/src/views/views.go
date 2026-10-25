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

// Package views returns views and ends them on behalf of its callers: through directives on a
// type of its own, and through helpers that wrap the standard library's views.
package views

import (
	"bufio"
	"bytes"
)

// Buffer is a byte buffer whose views stay valid until the next Reset.
type Buffer struct{ b []byte }

// Bytes returns a view of b. A declared view is ended by every event: 0xf.
//
//memsafe:viewof b
func (b *Buffer) Bytes() []byte { return b.b } // want Bytes:`^viewof\(0: 0xf\)$`

// Reset ends every view of b.
//
//memsafe:invalidates b
func (b *Buffer) Reset() { b.b = b.b[:0] } // want Reset:`^ends\(0: 0xf\)$`

// Write declares no contract, and calls nothing that ends a view: it gets no fact.
func (b *Buffer) Write(p []byte) { b.b = append(b.b, p...) }

// Contents returns the view bytes.Buffer.Bytes makes, which a read, a write, or a clear ends.
func Contents(buf *bytes.Buffer) []byte { return buf.Bytes() } // want Contents:`^viewof\(0: 0x7\)$`

// Head reslices the view, which keeps it a view.
func Head(buf *bytes.Buffer) []byte { return buf.Bytes()[:1] } // want Head:`^viewof\(0: 0x7\)$`

// Through returns another helper's view.
func Through(buf *bytes.Buffer) []byte { return Contents(buf) } // want Through:`^viewof\(0: 0x7\)$`

// Second returns a view into its second argument.
func Second(_, b *bytes.Buffer) []byte { return b.Bytes() } // want Second:`^viewof\(1: 0x7\)$`

// NilOrView returns nil or a view.
func NilOrView(buf *bytes.Buffer) []byte { // want NilOrView:`^viewof\(0: 0x7\)$`
	if buf.Len() == 0 {
		return nil
	}

	return buf.Bytes()
}

// Merged returns one of two views, ended only by the events that end both: a write.
func Merged(c bool, buf *bytes.Buffer) []byte { // want Merged:`^viewof\(1: 0x2\)$`
	if c {
		return buf.Bytes()
	}

	return buf.AvailableBuffer()
}

// Line returns the view bufio.Reader.ReadSlice makes, and is itself a read on r.
func Line(r *bufio.Reader) ([]byte, error) { return r.ReadSlice('\n') } // want Line:`^viewof\(0: 0x1\) ends\(0: 0x1\)$`

// EitherBuffer returns a view into one of two buffers, so it is no view of either.
func EitherBuffer(c bool, a, b *bytes.Buffer) []byte {
	if c {
		return a.Bytes()
	}

	return b.Bytes()
}

// Copy returns a copy, which no call on buf ends.
func Copy(buf *bytes.Buffer) []byte { return bytes.Clone(buf.Bytes()) }

// Clear ends the views that a clear ends.
func Clear(buf *bytes.Buffer) { buf.Reset() } // want Clear:`^ends\(0: 0x4\)$`

// ClearThrough calls Clear.
func ClearThrough(buf *bytes.Buffer) { Clear(buf) } // want ClearThrough:`^ends\(0: 0x4\)$`

// MaybeClear clears buf on some paths only, so it ends nothing.
func MaybeClear(c bool, buf *bytes.Buffer) {
	if c {
		buf.Reset()
	}
}

// Fill writes into its second argument.
func Fill(_ int, buf *bytes.Buffer) { buf.WriteByte('x') } // want Fill:`^ends\(1: 0x2\)$`

// Emit writes into a bufio.Writer.
func Emit(w *bufio.Writer, s string) { _, _ = w.WriteString(s) } // want Emit:`^ends\(0: 0x2\)$`

// Renew replaces the buffer before clearing it: the caller's views keep the old one.
func Renew(buf *bytes.Buffer) {
	*buf = bytes.Buffer{}
	buf.Reset()
}

// Recycle declares that it ends every view of buf, more than its body shows.
//
//memsafe:invalidates buf
func Recycle(buf *bytes.Buffer) { buf.Truncate(0) } // want Recycle:`^ends\(0: 0xf\)$`

// Window declares its view, which replaces the one its body makes.
//
//memsafe:viewof buf
func Window(buf *bytes.Buffer) []byte { return buf.Bytes() } // want Window:`^viewof\(0: 0xf\)$`

// Swap declares that it ends the views of b, and its body ends some views of a.
//
//memsafe:invalidates b
func Swap(a, b *bytes.Buffer) { a.Reset() } // want Swap:`^ends\(0: 0x4, 1: 0xf\)$`

// Conn holds its buffer by value.
type Conn struct{ buf bytes.Buffer }

// Bytes returns a view of a field of c, not of c itself, so it gets no fact.
func (c *Conn) Bytes() []byte { return c.buf.Bytes() }

// Reset clears a field of c, which leaves the views of its other fields valid: no fact.
func (c *Conn) Reset() { c.buf.Reset() }

// Embeds promotes the methods of its buffer, which are calls on a field.
type Embeds struct{ bytes.Buffer }

// Clear calls a promoted method: no fact.
func (e *Embeds) Clear() { e.Reset() }
