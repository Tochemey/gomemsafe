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

package directive_test

import (
	"go/types"
	"testing"

	"github.com/tochemey/gomemsafe/internal/directive"
)

// TestLockKindIsDecidedByTheTypeName checks which types are locks: those named like a Mutex
// or an RWMutex, through a pointer too, and sync.Locker; not a type that merely has Lock
// methods, nor a basic type.
func TestLockKindIsDecidedByTheTypeName(t *testing.T) {
	sync := types.NewPackage("sync", "sync")
	named := func(pkg *types.Package, name string) types.Type {
		return types.NewNamed(types.NewTypeName(0, pkg, name, nil), types.NewStruct(nil, nil), nil)
	}
	locker := types.NewNamed(types.NewTypeName(0, sync, "Locker", nil), types.NewInterfaceType(nil, nil), nil)

	for _, tc := range []struct {
		typ    types.Type
		rw, ok bool
	}{
		{named(sync, "Mutex"), false, true},
		{named(sync, "RWMutex"), true, true},
		{types.NewPointer(named(sync, "RWMutex")), true, true},
		{named(types.NewPackage("p", "p"), "FancyMutex"), false, true},
		{locker, false, true},
		{named(types.NewPackage("p", "p"), "Guard"), false, false},
		{types.Typ[types.Int], false, false},
	} {
		rw, ok := directive.LockKind(tc.typ)
		if rw != tc.rw || ok != tc.ok {
			t.Errorf("LockKind(%s) = %v, %v; want %v, %v", tc.typ, rw, ok, tc.rw, tc.ok)
		}
	}
}

func TestStructOfLooksThroughOnePointer(t *testing.T) {
	st := types.NewStruct(nil, nil)

	if got, ok := directive.StructOf(types.NewPointer(st)); !ok || got != st {
		t.Error("StructOf(*struct{}) is not the struct")
	}

	if _, ok := directive.StructOf(types.NewPointer(types.NewPointer(st))); ok {
		t.Error("StructOf(**struct{}) found a struct")
	}

	if _, ok := directive.StructOf(types.Typ[types.String]); ok {
		t.Error("StructOf(string) found a struct")
	}
}

func TestGlobalKeyQualifiesByPackagePath(t *testing.T) {
	pkg := types.NewPackage("example.com/m/p", "p")
	v := types.NewVar(0, pkg, "mu", types.Typ[types.Int])

	if got := directive.GlobalKey(v); got != "example.com/m/p.mu" {
		t.Fatalf("GlobalKey = %q", got)
	}
}
