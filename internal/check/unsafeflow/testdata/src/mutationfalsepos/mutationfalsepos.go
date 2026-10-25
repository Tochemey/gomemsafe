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

// Package mutationfalsepos holds look-alike correct code: strings made with unsafe.String or
// unsafe.Slice whose bytes no write is known to change before a use. MS102 must stay silent.
package mutationfalsepos

import "unsafe"

type conn struct{ buf []byte }

func use(...any) {}

func usedBefore(b []byte) {
	s := unsafe.String(&b[0], len(b))
	use(s)
	b[0] = 'x'
}

// writtenBefore fills the buffer before making the string, which is how the string is meant to
// be built.
func writtenBefore(b []byte) {
	b[0] = 'x'
	s := unsafe.String(&b[0], len(b))
	use(s)
}

func copiedFirst(b []byte) {
	s := string(b)
	b[0] = 'x'
	use(s)
}

func pastTheEnd(b []byte) {
	s := unsafe.String(&b[0], 4)
	b[4] = 'x'
	use(s)
}

func beforeTheStart(b []byte) {
	s := unsafe.String(&b[2], 2)
	b[1] = 'x'
	use(s)
}

// laterByte: s may hold only its first byte, n == 1, so a write to b[1] may miss it.
func laterByte(b []byte, n int) {
	s := unsafe.String(&b[0], n)
	b[1] = 'x'
	use(s)
}

func variableIndex(b []byte, n, i int) {
	s := unsafe.String(&b[0], n)
	b[i] = 'x'
	use(s)
}

// copyFromTheStart: the copy may write fewer bytes than reach s, which starts at b[2].
func copyFromTheStart(b, src []byte, n int) {
	s := unsafe.String(&b[2], n)
	copy(b, src)
	use(s)
}

func throughPartialReslice(b []byte, i int) {
	s := unsafe.String(&b[0], 2)
	b[1:][i] = 'x'
	use(s)
}

// shiftedReslice writes b[2], past s: index 0 of b[2:] is not index 0 of b.
func shiftedReslice(b []byte) {
	s := unsafe.String(&b[0], 2)
	b[2:][0] = 'x'
	use(s)
}

// pastTheLength writes into the capacity beyond len(b), which s does not cover.
func pastTheLength(b []byte) {
	s := unsafe.String(unsafe.SliceData(b), len(b))
	b[:cap(b)][len(b)] = 'x'
	use(s)
}

func emptyView(b []byte) {
	s := unsafe.String(&b[0], 0)
	b[0] = 'x'
	use(s)
}

func lengthOnly(b []byte) int {
	s := unsafe.String(&b[0], len(b))
	b[0] = 'x'

	return len(s)
}

func emptyComparison(b []byte) bool {
	s := unsafe.String(&b[0], len(b))
	b[0] = 'x'

	return s != ""
}

func otherBuffer(a, b []byte) {
	s := unsafe.String(&a[0], len(a))
	b[0] = 'x'
	use(s)
}

func maybeWritten(c bool, b []byte) {
	s := unsafe.String(&b[0], len(b))
	if c {
		b[0] = 'x'
	}

	use(s)
}

func madeEachIteration(b []byte, n int) {
	for i := range n {
		s := unsafe.String(&b[0], len(b))
		use(s)
		b[0] = byte(i)
	}
}

func readOnly(s string) {
	v := unsafe.Slice(unsafe.StringData(s), len(s))
	use(v[0])
	use(s)
}

func writtenThenOnlyMeasured(s string) int {
	v := unsafe.Slice(unsafe.StringData(s), len(s))
	v[0] = 'x'

	return len(s)
}

func literal() {
	v := unsafe.Slice(unsafe.StringData("abc"), 3)
	use(v[0])
}

func otherArrayElement() {
	var a [8]byte
	s := unsafe.String(&a[0], 4)
	a[4] = 'x'
	use(s)
}

// fieldBuffer is missed rather than reported: c.buf is loaded at each use and could be another
// slice by the time of the write.
func fieldBuffer(c *conn) {
	s := unsafe.String(&c.buf[0], len(c.buf))
	c.buf[0] = 'x'
	use(s)
}

func readInto(b []byte, src []byte) {
	s := unsafe.String(&b[0], len(b))
	copy(src, b)
	use(s)
}

// captured is missed rather than reported: a function literal loads b at each use, so the
// write may be into another slice.
func captured(b []byte) {
	func() {
		s := unsafe.String(&b[0], len(b))
		b[0] = 'x'
		use(s)
	}()
}

// unknownPointer makes a string from a pointer of unknown origin: no buffer is known to share it.
func unknownPointer(p *byte, n int, b []byte) {
	s := unsafe.String(p, n)
	b[0] = 'x'
	use(s)
}

// bytesOfBytes is a []byte view of a []byte, which shares no string.
func bytesOfBytes(b []byte) {
	v := unsafe.Slice(&b[0], len(b))
	v[0] = 'x'
	use(b)
}

func clearedMap(b []byte, m map[string]int) {
	s := unsafe.String(&b[0], len(b))
	clear(m)
	use(s)
}
