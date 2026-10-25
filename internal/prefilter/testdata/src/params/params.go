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

// Package params passes parameters to calls in the ways the prefilter must tell apart.
package params

type Buf struct{ inner Inner }

type Inner struct{}

type Embeds struct{ Inner }

func (*Inner) Reset() {} // want `with parameters: `

func (*Buf) Reset() {} // want `with parameters: `

func (Buf) Len() int { return 0 } // want `with parameters: `

func take(*Buf) {} // want `with parameters: `

func other(int) {} // want `with parameters: `

func asArgument(b *Buf) { // want `with parameters: params.take`
	take(b)
	take((b))
	other(1)
}

func asReceiver(b *Buf) { b.Reset() } // want `with parameters: \(\*params.Buf\).Reset`

func (b *Buf) ofTheReceiver() { b.Reset() } // want `with parameters: \(\*params.Buf\).Reset`

func methodExpression(b *Buf) { (*Buf).Reset(b) } // want `with parameters: \(\*params.Buf\).Reset`

func fieldOfParam(b *Buf) { b.inner.Reset() } // want `with parameters: `

func promoted(e *Embeds) { e.Reset() } // want `with parameters: `

func implicitLoad(b *Buf) int { return b.Len() } // want `with parameters: `

func implicitAddress(b Buf) { b.Reset() } // want `with parameters: `

func local() { // want `with parameters: `
	var b Buf
	take(&b)
	b.Reset()
}

func inLiteral(b *Buf) { // want `with parameters: params.take`
	func() { take(b) }()
}
