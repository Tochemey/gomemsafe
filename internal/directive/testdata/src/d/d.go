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

// Package d holds well-formed and malformed //memsafe: directives.
package d

/* want `MS000: suppression requires a reason: //memsafe:allow MS301 <reason>` */ //memsafe:allow MS301

/* want `MS000: //memsafe:allow must name the check IDs it suppresses` */ //memsafe:allow

/* want `MS000: unknown check ID "MS999" in //memsafe:allow` */ //memsafe:allow MS999 reason

/* want `MS000: unknown directive //memsafe:alow` */ //memsafe:alow MS301 typo

/* want `MS000: //memsafe:owned is not supported yet by this version of gomemsafe` */ //memsafe:owned

//memsafe:allow MS301,MS302 both are allowed here for a reason
var x int

// A comment that merely mentions memsafe:allow is not a directive.
var y = x

/* want `MS000: //memsafe:viewof must be in the doc comment of a function or method declaration` */ //memsafe:viewof x

// Buffer holds bytes.
//
/* want `MS000: //memsafe:invalidates must be in the doc comment of a function or method declaration` */ //memsafe:invalidates b
type Buffer struct{ b []byte }

// Bytes returns a view of b.
//
//memsafe:viewof b
func (b *Buffer) Bytes() []byte { return b.b }

// Reset ends the views of b.
//
//memsafe:invalidates b
func (b *Buffer) Reset() { b.b = b.b[:0] }

// Swap declares contracts on two arguments, after the receiver.
//
//memsafe:viewof from
//memsafe:invalidates to
//memsafe:invalidates from
func (*Buffer) Swap(n int, from, to *Buffer) []byte { return nil }

// Unnamed counts its unnamed parameter as an argument.
//
//memsafe:viewof b
func Unnamed(int, b *Buffer) []byte { return nil }

// Malformed holds contracts that name no parameter, or the wrong one.
//
/* want `MS000: //memsafe:viewof must name one parameter of Malformed` */ //memsafe:viewof
/* want `MS000: //memsafe:invalidates must name one parameter of Malformed` */ //memsafe:invalidates b c
/* want `MS000: //memsafe:invalidates names c, which is not a parameter of Malformed` */ //memsafe:invalidates c
/* want `MS000: //memsafe:invalidates names _, which is not a parameter of Malformed` */ //memsafe:invalidates _
/* want `MS000: //memsafe:viewof needs a result to be the view, and Malformed returns nothing` */ //memsafe:viewof b
func Malformed(b *Buffer, _ int) {}

// Twice declares its view twice.
//
//memsafe:viewof a
/* want `MS000: //memsafe:viewof declares the view of Twice twice` */ //memsafe:viewof b
func Twice(a, b *Buffer) []byte { return nil }
