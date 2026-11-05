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

// Package appended uses owned and borrowed slices after an append on a prefix of the same
// backing array may have overwritten them. MS503 must report each one.
package appended

// Frame is owned.
//
//memsafe:owned
type Frame []byte

// Frames is owned.
//
//memsafe:owned
type Frames []*Frame

func use(...any) {}

func prefix(a Frame, i int) {
	b := append(a[:i], 'x')
	use(a, b) // want `MS503: a used after an append may have overwritten it`
}

func throughVariable(a Frame, i int) {
	head := a[:i]
	b := append(head, 'x')
	use(b)
	_ = a[i] // want `MS503: a used after an append`
}

func fromLow(a Frame) {
	_ = append(a[1:2], 'x')
	use(a[0]) // want `MS503: a used after`
}

func constantPrefix(a Frame) Frame {
	_ = append(a[:1], 'x')
	return a // want `MS503: a used after`
}

func written(a Frame, i int) {
	b := append(a[:i], 'x')
	a[i] = 'y' // want `MS503: a used after`
	use(b)
}

func ranged(a Frame) {
	_ = append(a[:0], 'x', 'y')
	for _, c := range a { // want `MS503: a used after`
		use(c)
	}
}

func onSomePath(a Frame, c bool) {
	if c {
		_ = append(a[:0], 'x')
	}

	use(a) // want `MS503: a used after`
}

func appendedTwice(a Frame) {
	b := append(a[:0], 'x')
	b = append(b, 'y')
	use(b, a) // want `MS503: a used after`
}

func appendedResult(a Frame, i int) {
	grown := append(a, 'x')
	_ = append(a[:i], 'y')
	use(grown) // want `MS503: grown used after`
}

func elements(fs Frames) {
	_ = append(fs[:1], nil)
	use(fs) // want `MS503: fs used after`
}

func converted(a Frame) {
	raw := []byte(a)
	_ = append(raw[:0], 'x')
	use(a) // want `MS503: a used after`
}

func asString(a Frame) string {
	_ = append(a[:0], 'x')
	return string(a) // want `MS503: a used after`
}

func cappedHigher(a Frame) {
	_ = append(a[:1:2], 'x')
	use(a) // want `MS503: a used after`
}

// cappedOther caps the prefix beyond its length: i+1 is not i+2.
func cappedOther(a Frame, i int) {
	_ = append(a[:i+1:i+2], 'x')
	use(a) // want `MS503: a used after`
}

// cappedOtherOp caps the prefix at a different operation on the same operands.
func cappedOtherOp(a Frame, i int) {
	_ = append(a[:i-1:i+1], 'x')
	use(a) // want `MS503: a used after`
}

// cappedLoaded caps the prefix at a bound that is neither a constant nor arithmetic.
func cappedLoaded(a Frame, n *int) {
	_ = append(a[:*n:*n], 'x')
	use(a) // want `MS503: a used after`
}

// reslicedAfter reslices the overwritten slice into a variable that may also be nil.
func reslicedAfter(a Frame, c bool) {
	_ = append(a[:0], 'x')
	s := a[:3] // want `MS503: a used after`
	if c {
		s = nil
	}

	use(s)
}

func local() {
	a := make(Frame, 4)
	_ = append(a[:2], 'x')
	use(a) // want `MS503: a used after`
}

func inLiteral() {
	f := func(a Frame) {
		_ = append(a[:0], 'x')
		use(a) // want `MS503: a used after`
	}
	f(nil)
}

// loopedPrefix overwrites a in every iteration and reads it in the next.
func loopedPrefix(a Frame) {
	for i := range 3 {
		use(a[i]) // want `MS503: a used after`
		_ = append(a[:i], 'x')
	}
}

// filterTwice appends two elements per kept one, so an append may reach elements not yet read.
func filterTwice(a Frame) Frame {
	out := a[:0]
	for _, c := range a { // want `MS503: a used after`
		out = append(out, c, c)
	}

	return out
}

// filterThenUse uses the filtered slice's original after the filter overwrote it.
func filterThenUse(a Frame) {
	out := a[:0]
	for _, c := range a {
		if c != 0 {
			out = append(out, c)
		}

		use(c)
	}

	use(out, a) // want `MS503: a used after`
}

// filterAgain filters a again after the first pass overwrote it.
func filterAgain(a Frame) {
	for range 2 {
		out := a[:0] // want `MS503: a used after`
		for _, c := range a {
			out = append(out, c)
		}

		use(out)
	}
}

// filterAgainFromStart filters a again from an empty prefix made once, before both passes.
func filterAgainFromStart(a Frame) {
	start := a[:0]
	for range 2 {
		out := start
		for _, c := range a { // want `MS503: a used after`
			out = append(out, c)
		}

		use(out)
	}
}

// filterReadAfter reads the current element again after appending, when the append may have
// written over it.
func filterReadAfter(a Frame) Frame {
	out := a[:0]
	for i := range a {
		out = append(out, a[i]+1)
		use(a[i]) // want `MS503: a used after`
	}

	return out
}

// filterOtherAppend appends to the prefix in a filter loop through another slice.
func filterOtherAppend(a Frame) Frame {
	out := a[:0]
	for _, c := range a { // want `MS503: a used after`
		out = append(out, c) // want `MS503: out used after`
		_ = append(a[:1], c)
	}

	return out
}

// filterBehind reads one element behind the loop index, which an append may have reached.
func filterBehind(a Frame) Frame {
	out := a[:0]
	for i, c := range a {
		if i > 0 && c != a[i-1] { // want `MS503: a used after`
			out = append(out, c)
		}
	}

	return out
}

// filterFarAhead reads ahead by a variable amount.
func filterFarAhead(a Frame, n int) Frame {
	out := a[:0]
	for i := range a {
		if i+n < len(a) && a[i+n] != 0 { // want `MS503: a used after`
			out = append(out, a[i])
		}
	}

	return out
}

// filterDoubled appends each kept element twice: an append may reach elements not yet read.
func filterDoubled(a Frame) Frame {
	out := a[:0]
	for _, c := range a { // want `MS503: a used after`
		out = append(out, c)
		out = append(out, c)
	}

	return out
}

// filterRestart sets the filtered slice back to a longer prefix, ahead of the elements read.
func filterRestart(a Frame) Frame {
	out := a[:0]
	for _, c := range a { // want `MS503: a used after`
		out = append(out, c)
		if c == 0 {
			out = a[:2]
		}
	}

	return out
}

// filterFromStart restarts from a prefix chosen before the loop, which may be longer.
func filterFromStart(a Frame, long bool) Frame {
	start := a[:0]
	if long {
		start = a[:2]
	}

	out := a[:0]
	for _, c := range a { // want `MS503: a used after`
		out = append(out, c) // want `MS503: out used after`
		if c == 0 {
			out = start
		}
	}

	return out
}

// filterAhead starts the filtered slice one element in.
func filterAhead(a Frame) Frame {
	out := a[:1]
	for _, c := range a { // want `MS503: a used after`
		out = append(out, c)
	}

	return out
}

// filterSpread appends a whole slice per element.
func filterSpread(a Frame, extra []byte) Frame {
	out := a[:0]
	for _, c := range a { // want `MS503: a used after`
		out = append(out, extra...)
		use(c)
	}

	return out
}

// filterPair appends the elements of a two-element array per element.
func filterPair(a Frame) Frame {
	out := a[:0]
	for _, c := range a { // want `MS503: a used after`
		pair := [2]byte{c, c}
		out = append(out, pair[:]...)
	}

	return out
}

// filterMerged filters a slice that may be one an append overwrote before the loop.
func filterMerged(a, b Frame, c bool) Frame {
	s := b
	if c {
		s = a
		_ = append(a[:0], 'x')
	}

	out := s[:0] // want `MS503: s used after`
	for _, x := range s {
		out = append(out, x)
	}

	return out
}

// twoSlices overwrites two slices, and uses the second.
func twoSlices(a, b Frame) {
	_ = append(a[:0], 'x')
	_ = append(b[:0], 'y')
	use(b) // want `MS503: b used after`
}

// convertedPrefix appends to a prefix converted to another slice type.
func convertedPrefix(a Frame) {
	_ = append([]byte(a[:1]), 'x')
	use(a) // want `MS503: a used after`
}

// Borrows is borrowed, and reports like an owned slice.
//
//memsafe:borrows b
func Borrows(b []byte, i int) {
	_ = append(b[:i], 'x')
	use(b) // want `MS503: b used after`
}
