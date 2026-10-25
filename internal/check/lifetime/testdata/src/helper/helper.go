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

// Release returns b to the pool on every path, and clears it first, which ends its views.
func Release(b *bytes.Buffer) { // want Release:`^invalidates\(0x1\) ends\(0: 0x4\)$`
	b.Reset()
	pool.Put(b)
}

// ReleaseTwice calls Release, so it also invalidates its argument and ends its views.
func ReleaseTwice(b *bytes.Buffer) { Release(b) } // want ReleaseTwice:`^invalidates\(0x1\) ends\(0: 0x4\)$`

// MaybeRelease returns b to the pool on some paths only, so it gets no summary.
func MaybeRelease(c bool, b *bytes.Buffer) {
	if c {
		pool.Put(b)
	}
}

// Close returns b to the pool. The receiver is argument 0, so b is argument 1.
func (*Owner) Close(b *bytes.Buffer) { pool.Put(b) } // want Close:`^invalidates\(0x2\)$`

// Pooled is a buffer that returns itself to its pool.
type Pooled struct{ bytes.Buffer }

var pooled = sync.Pool{New: func() any { return new(Pooled) }}

// GetPooled returns a Pooled from its pool.
func GetPooled() *Pooled { return pooled.Get().(*Pooled) }

// Release returns the receiver itself to the pool: argument 0.
func (p *Pooled) Release() { pooled.Put(p) } // want Release:`^invalidates\(0x1\)$`

// Pool mirrors go.uber.org/zap/internal/pool: a generic wrapper around sync.Pool.
type Pool[T any] struct{ pool sync.Pool }

// Put returns x to the pool: argument 1, after the receiver.
func (p *Pool[T]) Put(x T) { p.pool.Put(x) } // want Put:`^invalidates\(0x2\)$`

// Buffer mirrors zap's buffer.Buffer, which frees itself through a generic pool.
type Buffer struct{ bs []byte }

var buffers Pool[*Buffer]

// NewBuffer returns an empty buffer.
func NewBuffer() *Buffer { return &Buffer{} }

// Len returns the number of bytes in b.
func (b *Buffer) Len() int { return len(b.bs) }

// Free returns b to its pool through the generic wrapper.
func (b *Buffer) Free() { buffers.Put(b) } // want Free:`^invalidates\(0x1\)$`

// Recycle returns any value to a pool through the generic wrapper.
func Recycle[T any](x T) { // want Recycle:`^invalidates\(0x1\)$`
	var p Pool[T]
	p.Put(x)
}
