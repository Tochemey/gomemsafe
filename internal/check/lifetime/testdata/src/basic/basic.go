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

// Package basic holds uses after sync.Pool.Put that MS301 must report.
package basic

import (
	"bytes"
	"io"
	"sync"
)

var pool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

func straightLine(p string) {
	buf := pool.Get().(*bytes.Buffer)
	pool.Put(buf)
	buf.WriteString(p) // want `MS301: buf used after it was returned to sync.Pool`
}

// parenthesized names the value inside the parentheses.
func parenthesized(p string) {
	buf := pool.Get().(*bytes.Buffer)
	pool.Put((buf))
	buf.WriteString(p) // want `MS301: buf used after it was returned to sync.Pool`
}

func useInLoop(n int) {
	buf := pool.Get().(*bytes.Buffer)
	pool.Put(buf)

	for range n {
		buf.WriteByte('x') // want `MS301: buf used after it was returned to sync.Pool`
	}
}

func throughAlias(p string) {
	buf := pool.Get().(*bytes.Buffer)
	other := buf
	pool.Put(buf)
	other.WriteString(p) // want `MS301: buf used after it was returned to sync.Pool`
}

func everyBranch(c bool, p string) {
	buf := pool.Get().(*bytes.Buffer)
	if c {
		pool.Put(buf)
	} else {
		pool.Put(buf)
	}

	buf.WriteString(p) // want `MS301: buf used after it was returned to sync.Pool`
}

func returned() *bytes.Buffer {
	buf := pool.Get().(*bytes.Buffer)
	pool.Put(buf)

	return buf // want `MS301: buf used after it was returned to sync.Pool`
}

func doublePut() {
	buf := pool.Get().(*bytes.Buffer)
	pool.Put(buf)
	pool.Put(buf) // want `MS301: buf used after it was returned to sync.Pool`
}

func asArgument(p string) int {
	buf := pool.Get().(*bytes.Buffer)
	pool.Put(buf)

	return consume(p, buf) // want `MS301: buf used after it was returned to sync.Pool`
}

func insideClosure(p string) func() {
	return func() {
		buf := pool.Get().(*bytes.Buffer)
		pool.Put(buf)
		buf.WriteString(p) // want `MS301: buf used after it was returned to sync.Pool`
	}
}

// throughPhi reaches the use through a phi of two buffers that were both returned. It once
// crashed the check, because the phi itself is not an argument of any Put.
func throughPhi(c bool, p string) {
	a := pool.Get().(*bytes.Buffer)
	b := pool.Get().(*bytes.Buffer)

	var buf *bytes.Buffer

	if c {
		pool.Put(a)
		buf = a
	} else {
		pool.Put(b)
		buf = b
	}

	buf.WriteString(p) // want `MS301: (a|b) used after it was returned to sync.Pool`
}

// throughInterface uses the buffer as the receiver of an interface method call.
func throughInterface(p string) {
	buf := pool.Get().(*bytes.Buffer)
	pool.Put(buf)

	var w io.Writer = buf
	w.Write([]byte(p)) // want `MS301: buf used after it was returned to sync.Pool`
}

func consume(p string, b *bytes.Buffer) int { return len(p) + b.Len() }

// reportedOnce reports only the first use of a returned buffer.
func reportedOnce(p string) {
	buf := pool.Get().(*bytes.Buffer)
	pool.Put(buf)
	buf.WriteString(p) // want `MS301: buf used after it was returned to sync.Pool`
	buf.WriteString(p)
}

// resliced returns the buffer's backing array through a reslice, the usual way to empty a
// pooled slice.
func resliced() byte {
	buf := make([]byte, 8)
	pool.Put(buf[:0])

	return buf[0] // want `MS301: buf used after it was returned to sync.Pool`
}

// slicedBefore returns a slice taken before the Put, which shares the backing array.
func slicedBefore() []byte {
	buf := make([]byte, 8)
	head := buf[:4]
	pool.Put(buf)

	return append(head, 1) // want `MS301: buf used after it was returned to sync.Pool`
}

// methodExpression calls the method through its method expression: the receiver is the
// first argument.
func methodExpression(r, next *releaser) int {
	(*releaser).Release(r, next)

	return r.n // want `MS301: r used after the call to \(\*basic.releaser\).Release invalidated it`
}

type releaser struct{ n int }

func (r *releaser) Release(next *releaser) { pool.Put(r) }

// selectReceive is reported at the select keyword: the receive's channel is not looked up.
func selectReceive() {
	ch := make(chan int)
	pool.Put(ch)

	select { // want `MS301: ch used after it was returned to sync.Pool`
	case <-ch:
	default:
	}
}
