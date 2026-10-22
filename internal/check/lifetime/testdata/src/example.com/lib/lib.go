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

// Package lib has an import path that differs from its name, so diagnostics must name its
// functions by package name, not by path.
package lib

import "sync"

var pool = sync.Pool{New: func() any { return new(Buf) }}

// Buf is a pooled buffer.
type Buf struct{ n int }

// Get returns a buffer from the pool.
func Get() *Buf { return pool.Get().(*Buf) }

// Len returns the buffer's length.
func (b *Buf) Len() int { return b.n }

// Free returns b to the pool.
func (b *Buf) Free() { pool.Put(b) }

// Release returns b to the pool.
func Release(b *Buf) { pool.Put(b) }
