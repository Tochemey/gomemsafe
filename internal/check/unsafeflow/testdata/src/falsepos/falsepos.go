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

// Package falsepos holds correct unsafe code, and misuses that plain vet already reports.
// MS101 and MS103 report none of them: they cover only what vet cannot see.
package falsepos

import "unsafe"

type T struct{ a, b int64 }

func singleExpression(p *T) *int64 {
	return (*int64)(unsafe.Pointer(uintptr(unsafe.Pointer(p)) + unsafe.Offsetof(p.b)))
}

// conversionVetReports is vet's unsafeptr finding; MS101 does not repeat it.
func conversionVetReports(p *T) unsafe.Pointer {
	u := uintptr(unsafe.Pointer(p))

	return unsafe.Pointer(u)
}

func addToAPointer(p *T) unsafe.Pointer {
	return unsafe.Add(unsafe.Pointer(p), unsafe.Offsetof(p.b))
}

func addZeroToNil() unsafe.Pointer {
	return unsafe.Add(nil, 0)
}

// uintptrAsUintptr reads an integer as an integer.
func uintptrAsUintptr(u uintptr) uintptr {
	return *(*uintptr)(unsafe.Pointer(&u))
}

// pointerAsUintptr reads a pointer's bits as an integer, which creates no pointer.
func pointerAsUintptr(p *T) uintptr {
	return *(*uintptr)(unsafe.Pointer(&p))
}

// pointerAsPointer reinterprets one pointer type as another, which the garbage collector
// still tracks.
func pointerAsPointer(p *T) *int64 {
	return *(**int64)(unsafe.Pointer(&p))
}

func bytesAsInteger(b *[8]byte) uint64 {
	return *(*uint64)(unsafe.Pointer(b))
}

func fromAPointerVariable(up unsafe.Pointer) unsafe.Pointer {
	return *(*unsafe.Pointer)(up)
}

func address() unsafe.Pointer { return nil }

func fromACall() unsafe.Pointer {
	return *(*unsafe.Pointer)(address())
}

func fromAnotherPointerType(pp **T) *T {
	return *(**T)(unsafe.Pointer(pp))
}

// uintptrAsStruct reads past the uintptr, a different bug, but creates no pointer.
func uintptrAsStruct(u uintptr) T {
	return *(*T)(unsafe.Pointer(&u))
}

func callThroughAnIndex(fns [1]func(int, int) int) int {
	return fns[0](1, 2)
}

// genericAnyTarget may be instantiated with a plain value, so nothing is known to be a
// pointer.
func genericAnyTarget[T any](u uintptr) T {
	return *(*T)(unsafe.Pointer(&u))
}

// genericMixed may be instantiated with *int, which is not a uintptr.
func genericMixed[U ~uintptr | ~*int](a *U) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(a))
}

type number interface{ ~int | ~uint }

func genericNumberTarget[N number](u uintptr) N {
	return *(*N)(unsafe.Pointer(&u))
}

func genericEmbeddedNumber[U interface{ number }](a *U) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(a))
}
