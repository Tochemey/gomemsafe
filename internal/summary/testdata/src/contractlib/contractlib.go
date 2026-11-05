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

// Package contractlib declares contracts on interface methods, and calls them.
package contractlib

// Frame is a value.
type Frame struct{ Data []byte }

// Store declares a contract on each method but the last.
type Store interface {
	//memsafe:consumes f
	Write(f *Frame) // want Write:`^consumes\(0x2\) retains\(0x3\)$`

	//memsafe:borrows p
	Peek(p []byte) // want Peek:`^retains\(0x1\)$`

	//memsafe:viewof f
	View(f *Frame) []byte // want View:`^viewof\(1: 0xf\) retains\(0x3\) returns\(0x2\)$`

	//memsafe:invalidates f
	Reset(f *Frame) // want Reset:`^ends\(1: 0xf\) retains\(0x3\)$`

	Plain(f *Frame)
}

func viewed(s Store, f *Frame) []byte { return s.View(f) } // want viewed:`^viewof\(1: 0xf\) retains\(0x3\) returns\(0x2\)$`

func reset(s Store, f *Frame) { s.Reset(f) } // want reset:`^ends\(1: 0xf\) retains\(0x3\)$`

func peeks(s Store, p []byte) { s.Peek(p) } // want peeks:`^retains\(0x1\)$`

func writes(s Store, f *Frame) { s.Write(f) } // want writes:`^retains\(0x3\)$`

func plain(s Store, f *Frame) { s.Plain(f) } // want plain:`^retains\(0x3\)$`
