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

// Package owner declares an owned type and a consuming function, which reach other packages as
// facts.
package owner

// Frame is owned.
//
//memsafe:owned
type Frame struct{ data []byte } // want Frame:`^owned$`

// Box is a generic owned holder.
//
//memsafe:owned
type Box[T any] struct{ v T } // want Box:`^owned$`

// Plain is not owned, and gets no fact.
type Plain struct{}

// Release takes ownership of f.
//
//memsafe:consumes f
func Release(f *Frame) { _ = f } // want Release:`^consumes\(0x1\)$`
