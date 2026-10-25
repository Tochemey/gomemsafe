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

// Package mutation writes into the bytes of a string made with unsafe.String or unsafe.Slice,
// then uses the string. MS102 must report each one.
package mutation

import "unsafe"

type name string

type buffer []byte

func use(...any) {}

func useString(string) {}

func firstByte(b []byte, n int) {
	s := unsafe.String(&b[0], n)
	b[0] = 'x'
	use(s) // want `MS102: s used after a write to the bytes it shares`
}

func whole(b []byte, i int) {
	s := unsafe.String(unsafe.SliceData(b), len(b))
	b[i] = 'x'
	use(s) // want `MS102: s used after a write to the bytes it shares`
}

func wholeByAddress(b []byte, i int) {
	s := unsafe.String(&b[0], len(b))
	b[i] = 'x'
	use(s) // want `MS102: s used after a write to the bytes it shares`
}

func insideConstantLength(b []byte) {
	s := unsafe.String(&b[2], 4)
	b[5] = 'x'
	use(s) // want `MS102: s used after a write to the bytes it shares`
}

func incremented(b []byte) {
	s := unsafe.String(&b[0], 1)
	b[0]++
	use(s) // want `MS102: s used after a write to the bytes it shares`
}

func copiedInto(b, src []byte) {
	s := unsafe.String(unsafe.SliceData(b), len(b))
	copy(b, src)
	use(s) // want `MS102: s used after a write to the bytes it shares`
}

func copiedAtStart(b, src []byte, n int) {
	s := unsafe.String(&b[0], n)
	copy(b, src)
	use(s) // want `MS102: s used after a write to the bytes it shares`
}

func cleared(b []byte, n int) {
	s := unsafe.String(&b[1], n)
	clear(b)
	use(s) // want `MS102: s used after a write to the bytes it shares`
}

func throughReslice(b []byte, i int) {
	s := unsafe.String(unsafe.SliceData(b), len(b))
	b[1:][i] = 'x'
	use(s) // want `MS102: s used after a write to the bytes it shares`
}

func array() {
	var a [4]byte
	s := unsafe.String(&a[0], len(a))
	a[1] = 'x'
	use(s) // want `MS102: s used after a write to the bytes it shares`
}

func arrayReplaced() {
	var a [4]byte
	s := unsafe.String(&a[0], 2)
	a = [4]byte{'x'}
	use(s) // want `MS102: s used after a write to the bytes it shares`
}

func fromString(s string) {
	v := unsafe.Slice(unsafe.StringData(s), len(s))
	v[0] = 'x'
	use(s) // want `MS102: s used after a write to the bytes it shares`
}

func fromStringPrefix(s string, n int) {
	v := unsafe.Slice(unsafe.StringData(s), n)
	clear(v)
	use(s) // want `MS102: s used after a write to the bytes it shares`
}

func resliced(b []byte) {
	s := unsafe.String(&b[0], len(b))
	b[0] = 'x'
	use(s[1:]) // want `MS102: s used after a write to the bytes it shares`
}

func named(b []byte) {
	s := name(unsafe.String(&b[0], len(b)))
	b[0] = 'x'
	use(s) // want `MS102: s used after a write to the bytes it shares`
}

func mapKey(b []byte, m map[string]int) int {
	s := unsafe.String(&b[0], len(b))
	b[0] = 0

	return m[s] // want `MS102: s used after a write to the bytes it shares`
}

func compared(b []byte) bool {
	s := unsafe.String(&b[0], len(b))
	b[0] = 0

	return s == "abc" // want `MS102: s used after a write to the bytes it shares`
}

func indexed(b []byte) byte {
	s := unsafe.String(&b[0], len(b))
	b[0] = 0

	return s[0] // want `MS102: s used after a write to the bytes it shares`
}

func bothPaths(c bool, b []byte) {
	s := unsafe.String(&b[0], len(b))
	if c {
		b[0] = 1
	} else {
		b[1] = 2
	}

	use(s) // want `MS102: s used after a write to the bytes it shares`
}

func reportedOnce(b []byte) {
	s := unsafe.String(&b[0], len(b))
	b[0] = 0
	use(s) // want `MS102: s used after a write to the bytes it shares`
	use(s)
}

func inLiteral() {
	func() {
		b := make([]byte, 4)
		s := unsafe.String(&b[0], len(b))
		b[0] = 0
		use(s) // want `MS102: s used after a write to the bytes it shares`
	}()
}

func asserted(b []byte) {
	var i any = unsafe.String(&b[0], len(b))
	b[0] = 'x'
	useString(i.(string)) // want `MS102: i\.\(string\) used after a write to the bytes it shares`
}

func throughNamedType(b []byte) {
	s := unsafe.String(&b[0], len(b))
	buffer(b)[0] = 'x'
	use(s) // want `MS102: s used after a write to the bytes it shares`
}
