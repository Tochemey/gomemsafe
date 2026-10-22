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

// Package position holds uses whose SSA position is an operator, not the value: the
// diagnostic must still point at the value.
package position

import "sync"

var pool sync.Pool

type box struct{ n int }

func field() int {
	b := new(box)
	pool.Put(b)

	return b.n // want `MS301: b used after it was returned to sync.Pool`
}

func comparison(other *box) bool {
	b := new(box)
	pool.Put(b)

	return other == b // want `MS301: b used after it was returned to sync.Pool`
}

func index() byte {
	s := make([]byte, 4)
	pool.Put(s)

	return s[0] // want `MS301: s used after it was returned to sync.Pool`
}

func deref() box {
	b := new(box)
	pool.Put(b)

	return *b // want `MS301: b used after it was returned to sync.Pool`
}

func slice() []byte {
	s := make([]byte, 4)
	pool.Put(s)

	return s[1:] // want `MS301: s used after it was returned to sync.Pool`
}

func comparedFirst(other *box) bool {
	b := new(box)
	pool.Put(b)

	return b == other // want `MS301: b used after it was returned to sync.Pool`
}

func mapKey(m map[*box]int) int {
	b := new(box)
	pool.Put(b)

	return m[b] // want `MS301: b used after it was returned to sync.Pool`
}

func mapValue() int {
	m := map[string]int{}
	pool.Put(m)

	return m["k"] // want `MS301: m used after it was returned to sync.Pool`
}

func receive() int {
	ch := make(chan int, 1)
	pool.Put(ch)

	return <-ch // want `MS301: ch used after it was returned to sync.Pool`
}
