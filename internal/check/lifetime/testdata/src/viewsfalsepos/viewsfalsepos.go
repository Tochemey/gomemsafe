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

// Package viewsfalsepos holds correct code that uses standard library views. MS302 reports none
// of it.
package viewsfalsepos

import (
	"bufio"
	"bytes"
	"strconv"
)

func use([]byte) {}

func copied(buf *bytes.Buffer) {
	b := bytes.Clone(buf.Bytes())
	buf.Reset()
	use(b)
}

func usedFirst(buf *bytes.Buffer) {
	b := buf.Bytes()
	use(b)
	buf.Reset()
}

func converted(buf *bytes.Buffer) string {
	s := string(buf.Bytes())
	buf.Reset()

	return s
}

func text(sc *bufio.Scanner) string {
	s := sc.Text()
	sc.Scan()

	return s
}

// lengthOnly reads the view's length, which the ended buffer does not change.
func lengthOnly(buf *bytes.Buffer) int {
	b := buf.Bytes()
	buf.Reset()

	return len(b)
}

func eachToken(sc *bufio.Scanner) {
	for sc.Scan() {
		use(sc.Bytes())
	}
}

func tokenPerIteration(sc *bufio.Scanner) {
	for sc.Scan() {
		tok := sc.Bytes()
		use(tok)
	}
}

func keepLastCopy(sc *bufio.Scanner) {
	var last []byte
	for sc.Scan() {
		last = bytes.Clone(sc.Bytes())
	}
	use(last)
}

// newBufferEachIteration keeps a view of the previous iteration's buffer, which the writes to
// this iteration's buffer do not touch.
func newBufferEachIteration(xs []string) {
	var prev []byte
	for _, x := range xs {
		var buf bytes.Buffer
		buf.WriteString(x)
		use(prev)
		prev = buf.Bytes()
	}
}

func rangedBuffers(bufs []*bytes.Buffer) {
	var prev []byte
	for _, buf := range bufs {
		buf.WriteByte('x')
		use(prev)
		prev = buf.Bytes()
	}
}

func scannerEachIteration(rs []*bufio.Reader) {
	var prev []byte
	for _, r := range rs {
		sc := bufio.NewScanner(r)
		sc.Scan()
		use(prev)
		prev = sc.Bytes()
	}
}

// correlatedMerge uses data only when it is nil.
func correlatedMerge(fast bool, buf *bytes.Buffer) {
	var data []byte
	if fast {
		data = buf.Bytes()
		buf.Reset()
	}

	if !fast {
		use(data)
	}
}

// clearedNext resets the buffer: the Next documentation ends the slice at a read or write only.
func clearedNext(buf *bytes.Buffer) {
	hdr := buf.Next(4)
	buf.Reset()
	use(hdr)
}

// grownAvailable grows the buffer: no view's documentation names Grow.
func grownAvailable(buf *bytes.Buffer) {
	b := buf.AvailableBuffer()
	buf.Grow(64)
	use(b)
}

func otherBuffer(a, b *bytes.Buffer) {
	v := a.Bytes()
	b.Reset()
	use(v)
}

// appendAndWrite is the idiom AvailableBuffer exists for: the write takes the view.
func appendAndWrite(buf *bytes.Buffer) {
	b := buf.AvailableBuffer()
	b = strconv.AppendInt(b, 42, 10)
	buf.Write(b)
}

// readAfterAvailable reads, and the documentation ends AvailableBuffer only at a write.
func readAfterAvailable(buf *bytes.Buffer) {
	b := buf.AvailableBuffer()
	_, _ = buf.ReadByte()
	use(b)
}

// readAfterReadLine reads a byte: the documentation ends ReadLine only at the next ReadLine.
func readAfterReadLine(r *bufio.Reader) {
	line, _, _ := r.ReadLine()
	_, _ = r.ReadByte()
	use(line)
}

// peekAfterPeek peeks again: the documentation does not count Peek as a read operation.
func peekAfterPeek(r *bufio.Reader) {
	a, _ := r.Peek(1)
	_, _ = r.Peek(2)
	use(a)
}

func flushed(w *bufio.Writer) {
	b := w.AvailableBuffer()
	_ = w.Flush()
	use(b)
}

func replaced(buf *bytes.Buffer) {
	b := buf.Bytes()
	buf.Reset()
	b = buf.Bytes()
	use(b)
}
