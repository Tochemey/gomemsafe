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

// Package suppress silences MS501, MS502, and MS503 with a reason.
package suppress

// Packet is owned.
//
//memsafe:owned
type Packet struct{ body []byte }

func allowed(out chan *Packet, p *Packet) int {
	out <- p

	//memsafe:allow MS501 the receiver only reads the packet, and never before this returns
	return len(p.body)
}

var last *Packet

// remembered keeps p, for a reason.
//
//memsafe:borrows p
func remembered(p *Packet) {
	//memsafe:allow MS502 the last packet is kept for diagnostics only
	last = p
}

// Frame is owned.
//
//memsafe:owned
type Frame []byte

// trimmed reads only the first byte after the append, which writes at the second.
func trimmed(f Frame) byte {
	_ = append(f[:1], 'x')

	//memsafe:allow MS503 the append writes f[1] and only f[0] is read
	return f[0]
}
