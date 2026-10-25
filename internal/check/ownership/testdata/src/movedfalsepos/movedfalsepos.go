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

// Package movedfalsepos holds look-alike correct code: owned values used before they move, or
// moved and then replaced, and values that never move. MS501 must stay silent.
package movedfalsepos

import "framelib"

// Packet is owned.
//
//memsafe:owned
type Packet struct {
	body []byte
	self *Packet
}

type plain struct{ p *Packet }

var shared Packet

func use(...any) {}

// keepSecond takes ownership of b only.
//
//memsafe:consumes b
func keepSecond(a, b *Packet) { _, _ = a, b }

func usedBefore(out chan *Packet, p *Packet) {
	use(p)
	out <- p
}

func replaced(out chan *Packet, p *Packet) {
	out <- p
	p = &Packet{}
	use(p)
}

func assignedAgain(out chan Packet) {
	var p Packet
	q := &p
	_ = q
	out <- p
	p = Packet{}
	use(p.body)
}

// sentOldContent sends the variable's old content after giving it a new one, which it still
// owns.
func sentOldContent(out chan Packet) {
	var p Packet
	q := &p
	_ = q
	old := p
	p = Packet{}
	out <- old
	use(p.body)
}

func replacedEachIteration(out chan *Packet, p *Packet, n int) {
	for range n {
		out <- p
		p = &Packet{}
	}

	use(p)
}

func newEachIteration(out chan *Packet, n int) {
	for range n {
		p := &Packet{}
		out <- p
	}
}

func otherCase(out chan *Packet, done chan struct{}, p *Packet) {
	select {
	case out <- p:
	case <-done:
		use(p)
	}
}

func deferred(f *framelib.Frame) {
	defer framelib.Release(f)
	use(f)
}

func notConsuming(f *framelib.Frame) {
	framelib.Inspect(f)
	use(f)
}

func recycled(f *framelib.Frame) {
	f = framelib.Recycle(f)
	use(f)
}

func notOwnedSent(out chan []byte, b []byte) {
	out <- b
	use(b)
}

func storedIntoNonOwner(pl *plain, p *Packet) {
	pl.p = p
	use(p)
}

func storedIntoItself(p *Packet) {
	p.self = p
	use(p)
}

func nilCheckedAfter(out chan *Packet, p *Packet) bool {
	out <- p

	return p == nil
}

func otherPacket(out chan *Packet, a, b *Packet) {
	out <- a
	use(b)
}

func consumedNil() {
	framelib.Release(nil)
}

func otherArgument(a, b *Packet) {
	keepSecond(a, b)
	use(a)
}

// consumedGlobal is missed rather than reported: the function does not make the global, so it
// cannot tell when the global was owned.
func consumedGlobal() {
	keepSecond(nil, &shared)
	use(shared.body)
}

func storedIntoReceivedNonOwner(plains chan *plain, p *Packet) {
	(<-plains).p = p
	use(p)
}
