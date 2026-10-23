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

// Package safety holds unsafe operations with and without a // SAFETY: comment. With
// -unsafeflow.safety, MS104 must report each one without a justification, once per statement.
package safety

import "unsafe"

type T struct{ a, b int64 }

var global int64

var unjustifiedGlobal = unsafe.Pointer(&global) // want `MS104: conversion to unsafe.Pointer without a // SAFETY: comment`

// SAFETY: global lives for the whole program.
var justifiedGlobal = unsafe.Pointer(&global)

var (
	// SAFETY: global lives for the whole program.
	justifiedInGroup = unsafe.Pointer(&global)

	unjustifiedInGroup = unsafe.Pointer(&global) // want `MS104: conversion to unsafe.Pointer`
)

func unjustified(b []byte) string {
	return unsafe.String(&b[0], len(b)) // want `MS104: unsafe.String without a // SAFETY: comment`
}

func justified(b []byte) string {
	// SAFETY: b is never written after this point.
	return unsafe.String(&b[0], len(b))
}

// justifiedAfterOtherText has its SAFETY: line after an explanation in the same group.
func justifiedAfterOtherText(b []byte) string {
	// The bytes come from a read-only file mapping.
	// SAFETY: nothing writes to the mapping.
	return unsafe.String(&b[0], len(b))
}

func fromPointer(p unsafe.Pointer) *T {
	return (*T)(p) // want `MS104: conversion from unsafe.Pointer`
}

func add(p unsafe.Pointer) unsafe.Pointer {
	return unsafe.Add(p, 8) // want `MS104: unsafe.Add`
}

func slice(p *byte, n int) []byte {
	return unsafe.Slice(p, n) // want `MS104: unsafe.Slice`
}

func sliceData(b []byte) *byte {
	return unsafe.SliceData(b) // want `MS104: unsafe.SliceData`
}

func stringData(s string) *byte {
	return unsafe.StringData(s) // want `MS104: unsafe.StringData`
}

// oncePerStatement reports the first of the statement's two operations only.
func oncePerStatement(p *T) *int64 {
	return (*int64)(unsafe.Pointer(p)) // want `MS104: conversion from unsafe.Pointer`
}

// multiLine is justified by the comment above the statement's first line.
func multiLine(p *T) *int64 {
	// SAFETY: b is a field of the object p points to.
	return (*int64)(unsafe.Add(
		unsafe.Pointer(p),
		unsafe.Offsetof(p.b),
	))
}

func notDirectlyAbove(b []byte) string {
	// SAFETY: b is never written after this point.

	return unsafe.String(&b[0], len(b)) // want `MS104: unsafe.String`
}

func noReason(b []byte) string {
	// SAFETY:
	return unsafe.String(&b[0], len(b)) // want `MS104: unsafe.String`
}

func wrongCase(b []byte) string {
	// Safety: b is never written after this point.
	return unsafe.String(&b[0], len(b)) // want `MS104: unsafe.String`
}

// ownStatement shows that a statement inside a function literal needs its own justification.
func ownStatement(p *T) func() unsafe.Pointer {
	// SAFETY: p outlives the closure.
	return func() unsafe.Pointer {
		return unsafe.Pointer(p) // want `MS104: conversion to unsafe.Pointer`
	}
}

func ifCondition(p *T) bool {
	// SAFETY: the conversion is only compared with nil.
	if unsafe.Pointer(p) == nil {
		return true
	}

	return false
}

// constants computes sizes and offsets only, which cannot break memory safety.
func constants(p *T) uintptr {
	var x unsafe.Pointer

	return unsafe.Sizeof(*p) + unsafe.Alignof(p.a) + unsafe.Offsetof(p.b) + unsafe.Sizeof(x)
}

func suppressed(p *T) unsafe.Pointer {
	//memsafe:allow MS104 generated code that cannot carry comments
	return unsafe.Pointer(p)
}

// ordinaryConversion converts without unsafe.Pointer.
func ordinaryConversion(p *T, n int) (*int64, int64) {
	return (*int64)(&p.a), int64(n)
}

// initAndCondition is reported once: one comment above the line justifies both the init
// statement and the if statement.
func initAndCondition(p *T) bool {
	if q := unsafe.Pointer(p); unsafe.Pointer(p) == q { // want `MS104: conversion to unsafe.Pointer`
		return true
	}

	return false
}
