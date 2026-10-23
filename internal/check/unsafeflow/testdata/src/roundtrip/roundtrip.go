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

// Package roundtrip turns uintptr values back into pointers in ways plain vet cannot see: no
// uintptr-to-unsafe.Pointer conversion appears. MS101 must report each one.
package roundtrip

import (
	"addresses"
	"reflect"
	"unsafe"
)

type T struct{ a, b int64 }

type holder struct{ addr uintptr }

// record has a uintptr field named like a reflect header's, which makes it no header.
type record struct{ Data uintptr }

// address is a named uintptr type.
type address uintptr

var saved uintptr

func punned(p *T) unsafe.Pointer {
	u := uintptr(unsafe.Pointer(p))

	return *(*unsafe.Pointer)(unsafe.Pointer(&u)) // want `MS101: u is a uintptr reinterpreted as a pointer`
}

func punnedToTypedPointer(p *T) *T {
	u := uintptr(unsafe.Pointer(p))

	return *(**T)(unsafe.Pointer(&u)) // want `MS101: u is a uintptr reinterpreted as a pointer`
}

func throughGlobal() *T {
	return *(**T)(unsafe.Pointer(&saved)) // want `MS101: saved is a uintptr reinterpreted as a pointer`
}

func throughField(h *holder) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&h.addr)) // want `MS101: h.addr is a uintptr reinterpreted as a pointer`
}

func throughNamedType(a address) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&a)) // want `MS101: a is a uintptr reinterpreted as a pointer`
}

func throughParens(u uintptr) unsafe.Pointer {
	return *((*unsafe.Pointer)((unsafe.Pointer)((&u)))) // want `MS101: u is a uintptr reinterpreted as a pointer`
}

func asMap(u uintptr) map[string]int {
	return *(*map[string]int)(unsafe.Pointer(&u)) // want `MS101: u is a uintptr reinterpreted as a pointer`
}

func addedToNil(p *T) unsafe.Pointer {
	u := uintptr(unsafe.Pointer(p))

	return unsafe.Add(nil, u) // want `MS101: unsafe.Add on a nil pointer turns an integer into a pointer`
}

func addedToConvertedNil(u uintptr) unsafe.Pointer {
	return unsafe.Add(unsafe.Pointer(nil), u) // want `MS101: unsafe.Add on a nil pointer turns an integer into a pointer`
}

func addedConstant() unsafe.Pointer {
	return unsafe.Add(nil, 0x1000) // want `MS101: unsafe.Add on a nil pointer turns an integer into a pointer`
}

func throughPointer(pu *uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(pu)) // want `MS101: \*pu is a uintptr reinterpreted as a pointer`
}

func asChannel(u uintptr) chan int {
	return *(*chan int)(unsafe.Pointer(&u)) // want `MS101: u is a uintptr reinterpreted as a pointer`
}

func asFunction(u uintptr) func() {
	return *(*func())(unsafe.Pointer(&u)) // want `MS101: u is a uintptr reinterpreted as a pointer`
}

// written stores a pointer into a uintptr, hiding it from the garbage collector.
func written(p unsafe.Pointer) uintptr {
	var u uintptr

	*(*unsafe.Pointer)(unsafe.Pointer(&u)) = p // want `MS101: u is a uintptr reinterpreted as a pointer`

	return u
}

func addedNegative() unsafe.Pointer {
	return unsafe.Add(nil, -8) // want `MS101: unsafe.Add on a nil pointer turns an integer into a pointer`
}

func addedToTypedNil(u uintptr) unsafe.Pointer {
	return unsafe.Add(unsafe.Pointer((*int)(nil)), u) // want `MS101: unsafe.Add on a nil pointer turns an integer into a pointer`
}

// addressConstraint is a named constraint of uintptr types.
type addressConstraint interface{ ~uintptr }

// pointerConstraint lists only pointer types.
type pointerConstraint interface{ ~*int | ~*int64 }

func genericUintptr[U ~uintptr](a *U) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(a)) // want `MS101: \*a is a uintptr reinterpreted as a pointer`
}

func genericExactUintptr[U interface{ uintptr }](a *U) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(a)) // want `MS101: \*a is a uintptr reinterpreted as a pointer`
}

func genericEmbeddedConstraint[U interface{ addressConstraint }](a *U) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(a)) // want `MS101: \*a is a uintptr reinterpreted as a pointer`
}

func genericTarget[P pointerConstraint](u uintptr) P {
	return *(*P)(unsafe.Pointer(&u)) // want `MS101: u is a uintptr reinterpreted as a pointer`
}

func throughOtherPackage() unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addresses.Saved)) // want `MS101: addresses.Saved is a uintptr reinterpreted as a pointer`
}

// throughReflectField reads a uintptr field of package reflect that is not a header's Data.
func throughReflectField(f *reflect.StructField) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&f.Offset)) // want `MS101: f.Offset is a uintptr reinterpreted as a pointer`
}

func throughFieldNamedData(r *record) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&r.Data)) // want `MS101: r.Data is a uintptr reinterpreted as a pointer`
}
