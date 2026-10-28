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

// Package chans communicates on channels and calls dynamic callees in every way the prefilter
// must notice, and has look-alikes that are neither.
package chans

import "io"

func send(ch chan int) { // want `communicates`
	ch <- 1
}

func receive(ch chan int) int { // want `communicates`
	return <-ch
}

func selects(a, b chan int) { // want `communicates`
	select {
	case a <- 1:
	case <-b:
	}
}

func ranges(ch chan int) { // want `communicates`
	for range ch {
	}
}

func inLiteral(ch chan int) { // want `communicates`
	go func() { ch <- 1 }()
}

func rangesOverSlice(s []int) int {
	n := 0
	for _, v := range s {
		n += -v
	}

	return n
}

func passesChannel(ch chan int) chan int {
	return ch
}

func callsInterface(w io.Writer) { // want `calls dynamic`
	_, _ = w.Write(nil)
}

func callsValue(f func()) { // want `calls dynamic`
	f()
}

func callsStatic(b []byte) bool {
	b = append(b, byte(len(b)))
	func() {}()

	return io.Writer(nil) == nil && passesChannel(nil) == nil && b != nil
}

var _ = func(ch chan int) { ch <- 1 }

func afterLiteral() {}
