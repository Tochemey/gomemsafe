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

package falsepos

import (
	"reflect"
	"unsafe"
)

// embedsPointer embeds a header pointer, which can point at a real slice.
type embedsPointer struct{ *reflect.SliceHeader }

// SliceHeader is a slice header of this package's own, with a Data field the garbage collector
// follows.
type SliceHeader struct {
	Data     unsafe.Pointer
	Len, Cap int
}

// overlaid reads a header pointer that points at a real slice, the form the reflect package
// allows.
func overlaid(b []byte) uintptr {
	return (*reflect.SliceHeader)(unsafe.Pointer(&b)).Data
}

// overlaidWrite points a real slice at a string's bytes through two header pointers.
func overlaidWrite(s string) []byte {
	var b []byte

	bh := (*reflect.SliceHeader)(unsafe.Pointer(&b))
	sh := (*reflect.StringHeader)(unsafe.Pointer(&s))
	bh.Data = sh.Data
	bh.Len = sh.Len
	bh.Cap = sh.Len

	return b
}

// overlaidDataAsPointer reads a real slice's pointer word, which the garbage collector tracks.
func overlaidDataAsPointer(b []byte) unsafe.Pointer {
	hp := (*reflect.SliceHeader)(unsafe.Pointer(&b))

	return *(*unsafe.Pointer)(unsafe.Pointer(&hp.Data))
}

// headerPointer may point at a real slice: nothing says it does not.
func headerPointer(hp *reflect.SliceHeader) []byte {
	return *(*[]byte)(unsafe.Pointer(hp))
}

// headerValueRead copies a header out but only reads its length, which creates no pointer.
func headerValueRead(b []byte) int {
	h := *(*reflect.SliceHeader)(unsafe.Pointer(&b))

	return h.Len
}

// headerDataToPointer is vet's unsafeptr finding; MS103 does not repeat it.
func headerDataToPointer(h reflect.SliceHeader) unsafe.Pointer {
	return unsafe.Pointer(h.Data)
}

// headerDataAsUintptr reads a uintptr as a uintptr.
func headerDataAsUintptr(h reflect.SliceHeader) uintptr {
	return *(*uintptr)(unsafe.Pointer(&h.Data))
}

// valueOtherThanHeader takes the address of a reflect type that is not a header.
func valueOtherThanHeader(k reflect.Kind) *uint {
	return (*uint)(unsafe.Pointer(&k))
}

// ownHeader builds a slice from a header whose Data is an unsafe.Pointer, which is correct.
func ownHeader(p *byte, n int) []byte {
	h := SliceHeader{Data: unsafe.Pointer(p), Len: n, Cap: n}

	return *(*[]byte)(unsafe.Pointer(&h))
}

// embeddedPointerData reads a real slice's pointer word through an embedded header pointer.
func embeddedPointerData(b []byte) unsafe.Pointer {
	w := embedsPointer{(*reflect.SliceHeader)(unsafe.Pointer(&b))}

	return *(*unsafe.Pointer)(unsafe.Pointer(&w.Data))
}
