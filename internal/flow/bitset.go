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

// Package flow is the dataflow engine every check shares: a forward "must" analysis over an
// SSA function, where a fact holds at a point only if it holds on every path reaching it.
//
// A client describes its analysis as a Problem: the number of facts, a transfer function for
// ordinary instructions, and a rule for phi nodes, which see the out-state of each incoming
// edge. Solve computes the fixpoint over the reachable blocks; Walk then replays it,
// presenting the state before each instruction, which is where checks look for violations.
//
// The engine is built for speed (docs/design.md §7): facts are bitsets, blocks are visited
// from a work list in reverse postorder, buffers are reused between functions so a warm
// Solve allocates nothing, and a per-function budget ends pathological inputs with Unknown.
// Unknown means "no result", and clients must then report nothing.
package flow

import "math/bits"

// Bitset is a fixed-size set of small integers.
type Bitset struct{ words []uint64 }

// Reset sizes the set for n elements and empties it, reusing storage when possible.
func (b *Bitset) Reset(n int) {
	w := (n + 63) / 64
	if cap(b.words) < w {
		b.words = make([]uint64, w)
		return
	}

	b.words = b.words[:w]
	clear(b.words)
}

// Fill adds every element to the set.
func (b *Bitset) Fill() {
	for i := range b.words {
		b.words[i] = ^uint64(0)
	}
}

// Set adds i.
func (b *Bitset) Set(i int) { b.words[i/64] |= 1 << uint(i%64) }

// Clear removes i.
func (b *Bitset) Clear(i int) { b.words[i/64] &^= 1 << uint(i%64) }

// Has reports whether i is in the set.
func (b *Bitset) Has(i int) bool { return b.words[i/64]&(1<<uint(i%64)) != 0 }

// CopyFrom makes b equal to o. Both must have the same size.
func (b *Bitset) CopyFrom(o *Bitset) { copy(b.words, o.words) }

// IntersectWith removes from b every element not in o.
func (b *Bitset) IntersectWith(o *Bitset) {
	for i := range b.words {
		b.words[i] &= o.words[i]
	}
}

// Equal reports whether b and o hold the same elements.
func (b *Bitset) Equal(o *Bitset) bool {
	for i := range b.words {
		if b.words[i] != o.words[i] {
			return false
		}
	}

	return true
}

// Len returns the number of elements in the set.
func (b *Bitset) Len() int {
	n := 0
	for _, w := range b.words {
		n += bits.OnesCount64(w)
	}

	return n
}
