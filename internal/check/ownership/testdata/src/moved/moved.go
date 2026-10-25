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

// Package moved uses values after their ownership moved. MS501 must report each one.
package moved

import "framelib"

// Packet is owned.
//
//memsafe:owned
type Packet struct{ body []byte }

// Message is owned, and owns packets.
//
//memsafe:owned
type Message struct {
	packet *Packet
	byID   map[int]*Packet
	frames []*Packet
}

func use(...any) {}

func usePacket(*Packet) {}

// keep takes ownership of p.
//
//memsafe:consumes p
func keep(p *Packet) { _ = p }

func sent(out chan *Packet, p *Packet) {
	out <- p
	p.body[0] = 1 // want `MS501: p used after it was moved`
}

func sentByValue(out chan Packet) {
	p := Packet{body: make([]byte, 1)}
	out <- p
	p.body[0] = 1 // want `MS501: p used after it was moved`
}

func sentVariable(out chan Packet) {
	var p Packet
	q := &p
	_ = q
	out <- p
	p.body = nil // want `MS501: p used after it was moved`
}

func consumed(f *framelib.Frame) {
	framelib.Release(f)
	use(f) // want `MS501: f used after it was moved`
}

func consumedHere(p *Packet) {
	keep(p)
	use(p) // want `MS501: p used after it was moved`
}

func closedReceiver(f *framelib.Frame) {
	f.Close()
	use(f) // want `MS501: f used after it was moved`
}

func consumedByGoroutine(f *framelib.Frame) {
	go framelib.Release(f)
	use(f) // want `MS501: f used after it was moved`
}

func consumedNotOwned(b []byte) {
	framelib.Free(b)
	use(b) // want `MS501: b used after it was moved`
}

func storedIntoOwner(m *Message, p *Packet) {
	m.packet = p
	use(p) // want `MS501: p used after it was moved`
}

func storedIntoOwnersMap(m *Message, p *Packet) {
	m.byID[1] = p
	use(p) // want `MS501: p used after it was moved`
}

func builtOwner(p *Packet) *Message {
	m := &Message{packet: p}
	use(p) // want `MS501: p used after it was moved`

	return m
}

func maybeMoved(c bool, out chan *Packet, p *Packet) {
	if c {
		out <- p
	}

	use(p) // want `MS501: p used after it was moved`
}

// movedInLoop sends p again in the next iteration, after the first send moved it.
func movedInLoop(out chan *Packet, p *Packet, n int) {
	for range n {
		out <- p // want `MS501: p used after it was moved`
	}
}

func sentTwice(out chan *Packet, p *Packet) {
	out <- p
	out <- p // want `MS501: p used after it was moved`
}

func throughInterface(out chan any, p *Packet) {
	out <- p
	use(p) // want `MS501: p used after it was moved`
}

func merged(c bool, out chan *Packet, a, b *Packet) {
	x := a
	if c {
		x = b
	}

	out <- x
	use(x) // want `MS501: x used after it was moved`
}

// movedOnOnePath may leave x holding a, which moved.
func movedOnOnePath(c bool, out chan *Packet, a *Packet) {
	x := &Packet{}
	if c {
		out <- a
		x = a
	}

	use(x) // want `MS501: x used after it was moved`
}

func inTheSendCase(out chan *Packet, done chan struct{}, p *Packet) {
	select {
	case out <- p:
		use(p) // want `MS501: p used after it was moved`
	case <-done:
	}
}

func returned(out chan *Packet, p *Packet) *Packet {
	out <- p

	return p // want `MS501: p used after it was moved`
}

func genericOwned(out chan *framelib.Box[int], b *framelib.Box[int]) {
	out <- b
	use(b) // want `MS501: b used after it was moved`
}

func reportedOnce(out chan *Packet, p *Packet) {
	out <- p
	use(p) // want `MS501: p used after it was moved`
	use(p)
}

func inLiteral(out chan *Packet) {
	func() {
		p := &Packet{}
		out <- p
		use(p) // want `MS501: p used after it was moved`
	}()
}

func assertedBack(out chan *Packet, p *Packet) {
	var i any = p
	out <- p
	usePacket(i.(*Packet)) // want `MS501: i\.\(\*Packet\) used after it was moved`
}

// consumedThenMerged may leave c holding b, which Free took.
func consumedThenMerged(cond bool, b, other []byte) {
	framelib.Free(b)

	c := other
	if cond {
		c = b
	}

	use(c) // want `MS501: c used after it was moved`
}

func twoMoved(out chan *Packet, a, b *Packet) {
	out <- a
	out <- b
	use(a) // want `MS501: a used after it was moved`
}

func storedIntoReceived(messages chan *Message, p *Packet) {
	(<-messages).packet = p
	use(p) // want `MS501: p used after it was moved`
}

// storedIntoOwnerVariable stores into an owner that a variable holds, which is loaded first.
func storedIntoOwnerVariable(p *Packet) *Message {
	m := new(Message)
	q := &m
	_ = q
	m.packet = p
	use(p) // want `MS501: p used after it was moved`

	return m
}

// storedIntoNestedField stores into an element of a slice that the owner holds.
func storedIntoNestedField(m *Message, p *Packet) {
	m.frames[0] = p
	use(p) // want `MS501: p used after it was moved`
}
