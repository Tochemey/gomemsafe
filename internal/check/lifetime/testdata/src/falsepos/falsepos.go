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

// Package falsepos holds code that resembles use after Put but is correct, or that MS301
// deliberately does not track. Nothing here may be reported.
package falsepos

import (
	"bytes"
	"sync"
)

var pool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

type ownPool struct{}

func (ownPool) Put(any) {}

func onOneBranchOnly(c bool, p string) {
	buf := pool.Get().(*bytes.Buffer)
	if c {
		pool.Put(buf)
	}

	buf.WriteString(p)
}

func deferred(p string) {
	buf := pool.Get().(*bytes.Buffer)
	defer pool.Put(buf)

	buf.WriteString(p)
}

func reacquired(p string) {
	buf := pool.Get().(*bytes.Buffer)
	pool.Put(buf)
	buf = pool.Get().(*bytes.Buffer)
	buf.WriteString(p)
}

func putInClosureThatMayNotRun(p string) {
	buf := pool.Get().(*bytes.Buffer)
	release := func() { pool.Put(buf) }
	_ = release

	buf.WriteString(p)
}

func differentValue(p string) {
	a := pool.Get().(*bytes.Buffer)
	b := pool.Get().(*bytes.Buffer)
	pool.Put(a)
	b.WriteString(p)
}

func userTypeWithPut(p string) {
	var own ownPool

	buf := pool.Get().(*bytes.Buffer)
	own.Put(buf)
	buf.WriteString(p)
}

func nilComparison() bool {
	buf := pool.Get().(*bytes.Buffer)
	pool.Put(buf)

	return buf == nil
}

func freshValueEachIteration(n int) {
	for range n {
		buf := pool.Get().(*bytes.Buffer)
		buf.WriteByte('x')
		pool.Put(buf)
	}
}

// addressTaken is a documented limit: values loaded through a pointer are not tracked.
func addressTaken(p string) {
	buf := pool.Get().(*bytes.Buffer)
	ptr := &buf
	pool.Put(buf)
	(*ptr).WriteString(p)
}

func putNil() {
	pool.Put(nil)
}

// Values without pointers are copied into the pool's interface, so the caller's copy stays
// valid.
func copiedInt(n int) int {
	pool.Put(n)
	return n + 1
}

func copiedString(s string) int {
	pool.Put(s)
	return len(s)
}

type pair struct{ a, b int }

func copiedStruct(p pair) int {
	pool.Put(p)
	return p.a + p.b
}

func giveInt(n int) { pool.Put(n) }

func copiedThroughHelper(n int) int {
	giveInt(n)
	return n
}

type releaser struct{ n int }

func (r *releaser) Release(next *releaser) { pool.Put(r) }

// methodValue calls Release through a method value, whose SSA wrapper takes no receiver: the
// argument n must not be mistaken for the invalidated receiver.
func methodValue(a, n, other *releaser) int {
	other.Release(nil)

	release := a.Release
	release(n)

	return n.n
}
