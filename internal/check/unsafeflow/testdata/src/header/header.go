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

// Package header uses reflect.SliceHeader and reflect.StringHeader values as memory: their
// Data field is a uintptr the garbage collector does not follow. MS103 must report each one.
package header

import (
	"reflect"
	"unsafe"
)

type wrapper struct{ hdr reflect.SliceHeader }

// sliceHeader is an alias, which names the same type.
type sliceHeader = reflect.SliceHeader

func built(p *byte, n int) []byte {
	h := reflect.SliceHeader{Data: uintptr(unsafe.Pointer(p)), Len: n, Cap: n}

	return *(*[]byte)(unsafe.Pointer(&h)) // want `MS103: h is a reflect.SliceHeader value: the garbage collector does not follow its Data field`
}

// copiedOut copies the header of a real slice into a value before using it.
func copiedOut(s []uint64) []byte {
	h := *(*reflect.SliceHeader)(unsafe.Pointer(&s))
	h.Len *= 8
	h.Cap *= 8

	return *(*[]byte)(unsafe.Pointer(&h)) // want `MS103: h is a reflect.SliceHeader value`
}

func declared(p *byte, n int) string {
	var h reflect.StringHeader
	h.Data = uintptr(unsafe.Pointer(p))
	h.Len = n

	return *(*string)(unsafe.Pointer(&h)) // want `MS103: h is a reflect.StringHeader value`
}

func literal(data uintptr, n int) []byte {
	return *(*[]byte)(unsafe.Pointer(&reflect.SliceHeader{Data: data, Len: n, Cap: n})) // want `MS103: reflect.SliceHeader\{…\} is a reflect.SliceHeader value`
}

func field(w *wrapper) []byte {
	return *(*[]byte)(unsafe.Pointer(&w.hdr)) // want `MS103: w.hdr is a reflect.SliceHeader value`
}

func element(hs []reflect.SliceHeader) []byte {
	return *(*[]byte)(unsafe.Pointer(&hs[0])) // want `MS103: hs\[0\] is a reflect.SliceHeader value`
}

func parens(h reflect.SliceHeader) []byte {
	return *(*[]byte)((unsafe.Pointer)((&(h)))) // want `MS103: h is a reflect.SliceHeader value`
}

func aliased(h sliceHeader) []byte {
	return *(*[]byte)(unsafe.Pointer(&h)) // want `MS103: h is a reflect.SliceHeader value`
}

func passedOn(h reflect.StringHeader) {
	use(unsafe.Pointer(&h)) // want `MS103: h is a reflect.StringHeader value`
}

// dataReadAsPointer is MS103's, not MS101's: the uintptr is a header value's Data field.
func dataReadAsPointer(h reflect.SliceHeader) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&h.Data)) // want `MS103: h is a reflect.SliceHeader value`
}

func use(unsafe.Pointer) {}
