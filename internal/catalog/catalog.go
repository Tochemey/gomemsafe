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

// Package catalog is the single list of gomemsafe check IDs. An ID such as MS301 is stable:
// it appears in diagnostics, in //memsafe:allow suppressions, and in users' CI logs, so it is
// never reused or renumbered. The catalogue holds every ID, including those of checks that are
// not implemented yet.
//
// To add a check, add its ID here first, then implement it under internal/check and register
// its analyzer in package analyzers.
package catalog

import "slices"

// ID is a stable check identifier.
type ID string

// Implemented checks.
const (
	// MalformedDirective is a //memsafe: directive that cannot be parsed or is not supported.
	MalformedDirective ID = "MS000"
	// UnsafePointerRoundTrip is a uintptr turned back into a pointer in a way go vet's
	// unsafeptr cannot see.
	UnsafePointerRoundTrip ID = "MS101"
	// ReflectHeaderMisuse is a reflect.SliceHeader or reflect.StringHeader value used as
	// memory, whose Data field the garbage collector does not follow.
	ReflectHeaderMisuse ID = "MS103"
	// UnjustifiedUnsafe is an unsafe operation that no // SAFETY: comment justifies. It is
	// opt-in.
	UnjustifiedUnsafe ID = "MS104"
	// UseAfterInvalidation is a use of a value after a call took it back, such as a use after
	// sync.Pool.Put.
	UseAfterInvalidation ID = "MS301"
)

// Planned checks. Their IDs are reserved so that code can be annotated ahead of the check.
const (
	MutatedStringView     ID = "MS102"
	GoPointerToC          ID = "MS201"
	UseAfterCFree         ID = "MS202"
	CAllocationLeak       ID = "MS203"
	UseOfInvalidatedView  ID = "MS302"
	EscapingBorrowedView  ID = "MS303"
	UnsynchronizedCapture ID = "MS401"
	ConcurrentMapAccess   ID = "MS402"
	UnguardedField        ID = "MS403"
	UseAfterMove          ID = "MS501"
	EscapingBorrow        ID = "MS502"
	AppendAliasing        ID = "MS503"
	ForbiddenImport       ID = "MS901"
	UnguardedSharedState  ID = "MS902"
	UnspecificSuppression ID = "MS903"
)

// known lists every ID in the catalogue, implemented or planned, in catalogue order.
var known = []ID{
	MalformedDirective,
	UnsafePointerRoundTrip, MutatedStringView, ReflectHeaderMisuse, UnjustifiedUnsafe,
	GoPointerToC, UseAfterCFree, CAllocationLeak,
	UseAfterInvalidation, UseOfInvalidatedView, EscapingBorrowedView,
	UnsynchronizedCapture, ConcurrentMapAccess, UnguardedField,
	UseAfterMove, EscapingBorrow, AppendAliasing,
	ForbiddenImport, UnguardedSharedState, UnspecificSuppression,
}

// IsKnown reports whether id is in the catalogue.
func IsKnown(id string) bool {
	return slices.Contains(known, ID(id))
}

// String returns the ID as written in diagnostics.
func (id ID) String() string { return string(id) }
