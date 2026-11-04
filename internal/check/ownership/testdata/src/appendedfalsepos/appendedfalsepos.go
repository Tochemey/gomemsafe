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

// Package appendedfalsepos appends to owned and borrowed slices in ways that leave every slice
// it uses afterwards intact. MS503 must stay silent on all of it.
package appendedfalsepos

// Frame is owned.
//
//memsafe:owned
type Frame []byte

func use(...any) {}

// reassigned is the delete idiom: the old slice is not used again.
func reassigned(a Frame, i int) Frame {
	a = append(a[:i], a[i+1:]...)
	return a
}

// reset reuses the buffer from its start.
func reset(a Frame, data []byte) Frame {
	a = append(a[:0], data...)
	use(a)

	return a
}

func capped(a Frame, i int) {
	b := append(a[:i:i], 'x')
	use(a, b)
}

// cappedComputed caps the prefix at a bound computed twice.
func cappedComputed(a Frame, i int) {
	b := append(a[:i+1:i+1], 'x')
	use(a, b)
}

func cappedConstant(a Frame) {
	b := append(a[:0:0], 'x')
	use(a, b)
}

func whole(a Frame) {
	b := append(a, 'x')
	use(a, b)
}

func toLength(a Frame) {
	b := append(a[:len(a)], 'x')
	use(a, b)
}

func openEnded(a Frame) {
	b := append(a[1:], 'x')
	use(a, b)
}

// measured only asks for the length and capacity, which append does not change.
func measured(a Frame) int {
	_ = append(a[:0], 'x')
	return len(a) + cap(a)
}

func comparedWithNil(a Frame) bool {
	_ = append(a[:0], 'x')
	return a == nil
}

// usedBefore uses a before the append only.
func usedBefore(a Frame) Frame {
	use(a)

	return append(a[:0], 'x')
}

// prefixUsed uses the prefix itself after appending to it: its own elements are unchanged.
func prefixUsed(a Frame) {
	head := a[:0]
	b := append(head, 'x')
	use(b)
}

// fresh appends to a copy.
func fresh(a Frame) {
	b := append(Frame(nil), a[:1]...)
	b = append(b, 'x')
	use(a, b)
}

// otherSlice appends to a prefix of a slice that is not followed.
func otherSlice(a Frame, raw []byte) {
	_ = append(raw[:0], 'x')
	use(a, raw)
}

// filter is the in-place filter of the Go wiki's SliceTricks.
func filter(a Frame) Frame {
	out := a[:0]
	for _, c := range a {
		if c != 0 {
			out = append(out, c)
		}
	}

	return out
}

// filterByIndex ranges over the indexes and reads each element before appending it.
func filterByIndex(a Frame) Frame {
	out := a[:0]
	for i := range a {
		if a[i] != 0 {
			out = append(out, a[i])
		}
	}

	return out
}

// dedup reads one element ahead, which the appends have not reached either.
func dedup(a Frame) Frame {
	out := a[:0]
	for i, c := range a {
		if i == len(a)-1 || c != a[i+1] {
			out = append(out, c)
		}
	}

	return out
}

// filterAfterUse filters a slice whose earlier uses all came first.
func filterAfterUse(a Frame) Frame {
	use(a)

	out := a[:0]
	for _, c := range a {
		out = append(out, c)
	}

	return out
}

// reload reads a field again after the append: it is another slice.
type holder struct{ buf Frame }

func reload(h *holder) {
	h.buf = append(h.buf[:0], 'x')
	use(h.buf)
}

// reused appends to the same prefix in every iteration and never reads past it.
func reused(a Frame, names []string) {
	for _, n := range names {
		key := append(a[:1], n...)
		use(key)
	}
}

// Borrows appends to a copy of what it borrows.
//
//memsafe:borrows b
func Borrows(b []byte) []byte {
	out := append([]byte(nil), b...)
	out = append(out[:0], 'x')
	use(b)

	return out
}

// borrowsNothing takes a plain slice: without -ownership.appendalias, MS503 does not follow it.
func borrowsNothing(b []byte, i int) {
	_ = append(b[:i], 'x')
	use(b)
}
