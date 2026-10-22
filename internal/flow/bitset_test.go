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

package flow

import "testing"

func TestBitsetOperations(t *testing.T) {
	var a, b Bitset

	a.Reset(130)
	b.Reset(130)

	a.Set(0)
	a.Set(64)
	a.Set(129)

	if !a.Has(64) || a.Has(63) || a.Len() != 3 {
		t.Fatalf("after Set: Has(64)=%v Has(63)=%v Len=%d", a.Has(64), a.Has(63), a.Len())
	}

	a.Clear(64)

	if a.Has(64) || a.Len() != 2 {
		t.Fatalf("after Clear: Has(64)=%v Len=%d", a.Has(64), a.Len())
	}

	b.Fill()
	b.Clear(0)
	b.IntersectWith(&a)

	if b.Has(0) || !b.Has(129) || b.Len() != 1 {
		t.Fatalf("after IntersectWith: Has(0)=%v Has(129)=%v Len=%d", b.Has(0), b.Has(129), b.Len())
	}

	b.CopyFrom(&a)

	if !b.Equal(&a) {
		t.Fatal("CopyFrom: sets differ")
	}

	b.Set(5)

	if b.Equal(&a) {
		t.Fatal("Equal: different sets compare equal")
	}
}

func TestBitsetResetReusesStorage(t *testing.T) {
	var b Bitset

	b.Reset(256)
	b.Fill()
	b.Reset(64)

	if b.Len() != 0 || cap(b.words) < 4 {
		t.Fatalf("Reset(64) after Reset(256): Len=%d cap=%d, want an empty set reusing storage", b.Len(), cap(b.words))
	}
}
