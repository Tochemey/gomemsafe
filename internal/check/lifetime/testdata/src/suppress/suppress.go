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

// Package suppress checks that //memsafe:allow silences exactly what it names.
package suppress

import (
	"bytes"
	"sync"
)

var pool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

func allowedOnLineAbove(p string) {
	buf := pool.Get().(*bytes.Buffer)
	pool.Put(buf)
	//memsafe:allow MS301 this pool never hands the buffer out again
	buf.WriteString(p)
}

func allowedOnSameLine(p string) {
	buf := pool.Get().(*bytes.Buffer)
	pool.Put(buf)
	buf.WriteString(p) //memsafe:allow MS301 deliberate for this test
}

func otherIDDoesNotSuppress(p string) {
	buf := pool.Get().(*bytes.Buffer)
	pool.Put(buf)
	//memsafe:allow MS302 a different check
	buf.WriteString(p) // want `MS301`
}
