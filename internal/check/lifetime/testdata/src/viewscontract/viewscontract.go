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

// Package viewscontract makes and ends views through interface methods with contracts.
package viewscontract

// Frame is a value.
type Frame struct{ data []byte }

// Store declares its views.
type Store interface {
	// View returns a view into f.
	//
	//memsafe:viewof f
	View(f *Frame) []byte

	// Reset ends the views of f.
	//
	//memsafe:invalidates f
	Reset(f *Frame)

	// Touch says nothing of f.
	Touch(f *Frame)
}

func use([]byte) {}

func ended(s Store, f *Frame) {
	v := s.View(f)
	s.Reset(f)
	use(v) // want `MS302: v used after s.Reset ended the views into f`
}

func wrapped(s Store, f *Frame) {
	v := viewThrough(s, f)
	reset(s, f)
	use(v) // want `MS302: v used after reset ended the views into f`
}

func viewThrough(s Store, f *Frame) []byte { return s.View(f) }

func reset(s Store, f *Frame) { s.Reset(f) }

func usedFirst(s Store, f *Frame) {
	v := s.View(f)
	use(v)
	s.Reset(f)
}

func otherFrame(s Store, f, g *Frame) {
	v := s.View(f)
	s.Reset(g)
	use(v)
}

func notEnded(s Store, f *Frame) {
	v := s.View(f)
	s.Touch(f)
	use(v)
}
