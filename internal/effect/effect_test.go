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

package effect

import (
	"go/importer"
	"go/token"
	"go/types"
	"testing"
)

func TestOfKnowsPoolPut(t *testing.T) {
	inv, ok := Of(method(t, "sync", "Pool", "Put"))
	if !ok {
		t.Fatal("no known effect for (*sync.Pool).Put")
	}

	if !inv.Args.Has(1) || inv.Args.Has(0) {
		t.Fatalf("Args = %#x, want only argument 1 (the value; argument 0 is the receiver)", uint64(inv.Args))
	}

	if inv.Description != ReturnedToPool {
		t.Fatalf("Description = %q", inv.Description)
	}
}

func TestOfIgnoresOtherFunctions(t *testing.T) {
	if _, ok := Of(method(t, "sync", "Pool", "Get")); ok {
		t.Fatal("(*sync.Pool).Get has a known effect, want none")
	}

	if _, ok := Of(nil); ok {
		t.Fatal("Of(nil) found an effect")
	}
}

func TestArgSetHasRejectsOutOfRange(t *testing.T) {
	s := ArgSet(1<<0 | 1<<63)

	if !s.Has(0) || !s.Has(63) || s.Has(64) || s.Has(-1) || s.Has(1) {
		t.Fatalf("Has on %#x gave wrong answers", uint64(s))
	}
}

// method returns the method name of the named type typ in package path, type-checked from
// source so the test needs no export data.
func method(t *testing.T, path, typ, name string) *types.Func {
	t.Helper()

	pkg, err := importer.ForCompiler(token.NewFileSet(), "source", nil).Import(path)
	if err != nil {
		t.Fatal(err)
	}

	obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(pkg.Scope().Lookup(typ).Type()), true, pkg, name)

	fn, ok := obj.(*types.Func)
	if !ok {
		t.Fatalf("%s.%s.%s is not a method", path, typ, name)
	}

	return fn
}
