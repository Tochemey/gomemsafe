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

// Package framelib declares an owned type and consuming functions for the packages that use
// them, which learn of both through the summary analyzer's facts.
package framelib

// Frame is a buffer that one owner holds at a time.
//
//memsafe:owned
type Frame struct{ data []byte }

// Box is a generic owned holder.
//
//memsafe:owned
type Box[T any] struct{ v T }

// Release takes f back.
//
//memsafe:consumes f
func Release(f *Frame) { _ = f }

// Close ends f, its receiver.
//
//memsafe:consumes f
func (f *Frame) Close() { _ = f }

// Free takes b, which is not of an owned type.
//
//memsafe:consumes b
func Free(b []byte) { _ = b }

// Recycle takes f and returns a fresh frame.
//
//memsafe:consumes f
func Recycle(f *Frame) *Frame { _ = f; return &Frame{} }

// Inspect only reads f.
func Inspect(f *Frame) { _ = f }
