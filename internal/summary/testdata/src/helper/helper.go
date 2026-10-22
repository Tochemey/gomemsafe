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

// Package helper releases buffers on behalf of its callers.
package helper

import (
	"bytes"
	"sync"
)

var pool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// Owner releases buffers through a method.
type Owner struct{}

// Get returns a buffer from the pool.
func Get() *bytes.Buffer { return pool.Get().(*bytes.Buffer) }

// Release returns b to the pool on every path.
func Release(b *bytes.Buffer) { // want Release:`invalidates\(0x1\)`
	b.Reset()
	pool.Put(b)
}

// ReleaseTwice calls Release, so it also invalidates its argument.
func ReleaseTwice(b *bytes.Buffer) { Release(b) } // want ReleaseTwice:`invalidates\(0x1\)`

// MaybeRelease returns b to the pool on some paths only, so it gets no summary.
func MaybeRelease(c bool, b *bytes.Buffer) {
	if c {
		pool.Put(b)
	}
}

// Close returns b to the pool. The receiver is argument 0, so b is argument 1.
func (*Owner) Close(b *bytes.Buffer) { pool.Put(b) } // want Close:`invalidates\(0x2\)`

// Pooled is a buffer that returns itself to its pool.
type Pooled struct{ bytes.Buffer }

var pooled = sync.Pool{New: func() any { return new(Pooled) }}

// GetPooled returns a Pooled from its pool.
func GetPooled() *Pooled { return pooled.Get().(*Pooled) }

// Release returns the receiver itself to the pool: argument 0.
func (p *Pooled) Release() { pooled.Put(p) } // want Release:`invalidates\(0x1\)`
