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

// Package views uses standard library views after a call on their owner ended them. MS302
// must report each one.
package views

import (
	"bufio"
	"bytes"
	"fmt"
)

var shared bytes.Buffer

type conn struct{ buf bytes.Buffer }

type embeds struct{ bytes.Buffer }

func use([]byte) {}

func reset(buf *bytes.Buffer) {
	b := buf.Bytes()
	buf.Reset()
	use(b) // want `MS302: b used after buf.Reset ended the views into buf`
}

func scan(sc *bufio.Scanner) {
	tok := sc.Bytes()
	sc.Scan()
	use(tok) // want `MS302: tok used after sc.Scan ended the views into sc`
}

// keepLast keeps the last token, which the final Scan, the one that returned false, ended.
func keepLast(sc *bufio.Scanner) {
	var last []byte
	for sc.Scan() {
		last = sc.Bytes()
	}
	use(last) // want `MS302: last used after sc.Scan ended the views into sc`
}

func (c *conn) flush() {
	b := c.buf.Bytes()
	c.buf.Reset()
	use(b) // want `MS302: b used after c.buf.Reset ended the views into c.buf`
}

func (e *embeds) flush() {
	b := e.Bytes()
	e.Reset()
	use(b) // want `MS302: b used after e.Reset ended the views into e`
}

func peek(r *bufio.Reader, p []byte) {
	b, _ := r.Peek(4)
	_, _ = r.Read(p)
	use(b) // want `MS302: b used after r.Read ended the views into r`
}

func readSlice(r *bufio.Reader) {
	line, _ := r.ReadSlice('\n')
	_, _ = r.ReadString('\n')
	use(line) // want `MS302: line used after r.ReadString ended the views into r`
}

func readLine(r *bufio.Reader) {
	line, _, _ := r.ReadLine()
	_, _, _ = r.ReadLine()
	use(line) // want `MS302: line used after r.ReadLine ended the views into r`
}

func next(buf *bytes.Buffer) {
	head := buf.Next(2)
	buf.WriteString("x")
	use(head) // want `MS302: head used after buf.WriteString ended the views into buf`
}

func writtenThrough(buf *bytes.Buffer) {
	b := buf.Bytes()
	buf.WriteByte('x')
	b[0] = 'y' // want `MS302: b used after buf.WriteByte ended the views into buf`
}

func available(w *bufio.Writer) {
	b := w.AvailableBuffer()
	w.WriteString("x")
	use(b) // want `MS302: b used after w.WriteString ended the views into w`
}

func global() {
	b := shared.Bytes()
	shared.Reset()
	use(b) // want `MS302: b used after shared.Reset ended the views into shared`
}

func localBuffer() {
	var buf bytes.Buffer
	buf.WriteString("x")
	b := buf.Bytes()
	buf.Truncate(0)
	use(b[:1]) // want `MS302: b used after buf.Truncate ended the views into buf`
}

// inClosure is checked like any function. Its buffer is its own parameter: one it captured
// would be loaded from memory at each call, which the check does not follow.
func inClosure() func(*bytes.Buffer) {
	return func(buf *bytes.Buffer) {
		b := buf.Bytes()
		buf.Reset()
		use(b) // want `MS302: b used after buf.Reset ended the views into buf`
	}
}

func converted(buf *bytes.Buffer) string {
	b := buf.Bytes()
	buf.Reset()

	return string(b) // want `MS302: b used after buf.Reset ended the views into buf`
}

func returned(buf *bytes.Buffer) []byte {
	b := buf.Bytes()
	buf.Reset()

	return b // want `MS302: b used after buf.Reset ended the views into buf`
}

func sent(buf *bytes.Buffer, ch chan<- []byte) {
	b := buf.Bytes()
	buf.Reset()
	ch <- b // want `MS302: b used after buf.Reset ended the views into buf`
}

func inGoroutine(buf *bytes.Buffer) {
	b := buf.Bytes()
	buf.Reset()
	go use(b) // want `MS302: b used after buf.Reset ended the views into buf`
}

func printed(buf *bytes.Buffer) {
	b := buf.Bytes()
	buf.Reset()
	fmt.Println(b) // want `MS302: b used after buf.Reset ended the views into buf`
}

// comparedWithItself is the bug in the compress/lzw tests of the standard library: after
// Reset, the second run writes into the same array, so b1 holds the second run's bytes and the
// comparison cannot fail.
func comparedWithItself(b *bytes.Buffer, run func(*bytes.Buffer)) bool {
	run(b)
	b1 := b.Bytes()
	b.Reset()
	run(b)
	b2 := b.Bytes()

	return bytes.Equal(b1, b2) // want `MS302: b1 used after b.Reset ended the views into b`
}

// grownThenWritten is the standard library's ExampleBuffer_Grow. Grow keeps the write from
// allocating, so the slice happens to see it, but the Bytes documentation ends the view at
// the write: "valid for use only until the next buffer modification".
func grownThenWritten() {
	var b bytes.Buffer
	b.Grow(64)
	bb := b.Bytes()
	b.Write([]byte("64 bytes or fewer"))
	use(bb[:b.Len()]) // want `MS302: bb used after b.Write ended the views into b`
}

func suppressed(buf *bytes.Buffer) {
	b := buf.Bytes()
	buf.Reset()
	//memsafe:allow MS302 the test only checks that the slice is empty
	use(b)
}
