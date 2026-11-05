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

// Package contractlib declares an interface contract for other packages to call and implement.
package contractlib

// Frame is a value.
type Frame struct{ Data []byte }

// Sink declares a contract on each method that holds a parameter.
type Sink interface {
	// Write takes f.
	//
	//memsafe:consumes f
	Write(f *Frame)

	// Peek keeps nothing of p.
	//
	//memsafe:borrows p
	Peek(p []byte) int
}

// Keeper implements Sink, and breaks the contract of Peek.
type Keeper struct{ kept []byte }

// Write takes f, and may do anything with it.
func (k *Keeper) Write(f *Frame) { k.kept = f.Data }

// Peek keeps p.
func (k *Keeper) Peek(p []byte) int {
	k.kept = p
	return len(p)
}
