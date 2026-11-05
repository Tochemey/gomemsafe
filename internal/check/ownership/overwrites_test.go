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

package ownership

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

const overwritesSrc = `package p

func prefix(a []int, i int) { _ = append(a[:i], 1) }

func fromLow(a []int) { _ = append(a[1:2], 1) }

func capped(a []int, i int) { _ = append(a[:i:i], 1) }

func cappedConstant(a []int) { _ = append(a[:0:0], 1) }

func cappedHigher(a []int) { _ = append(a[:1:2], 1) }

func whole(a []int) { _ = append(a, 1) }

func toLength(a []int) { _ = append(a[:len(a)], 1) }

func openEnded(a []int) { _ = append(a[1:], 1) }

func onNil() { _ = append([]int(nil), 1) }

func throughAppend(a []int, i int) {
	t := append(a[:i], 1)
	_ = append(t, 2)
}

func throughPhi(a []int, c bool) {
	s := a[:1]
	if c {
		s = a[:2]
	}
	_ = append(s, 1)
}

func ofArray(arr *[4]int) { _ = append(arr[:2], 1) }

type ints []int

func converted(a ints) { _ = append([]int(a[:1]), 1) }

func uses(a []int) (int, bool, []int) {
	_ = append(a[:0], 1)
	return len(a) + cap(a), a == nil, a
}
`

// TestFindsTheAppendsThatOverwrite checks which appends may write into the backing array of
// another slice: those extending a prefix with a high bound that is not the length, and not
// capped there.
func TestFindsTheAppendsThatOverwrite(t *testing.T) {
	pkg := buildSource(t, overwritesSrc)

	for name, want := range map[string]int{
		"prefix": 1, "fromLow": 1, "cappedHigher": 1, "throughAppend": 2, "throughPhi": 1, "converted": 1,
		"capped": 0, "cappedConstant": 0, "whole": 0, "toLength": 0, "openEnded": 0, "onNil": 0, "ofArray": 0,
	} {
		a := newAliasing(pkg.Func(name), func(ssa.Value) bool { return true })

		got := 0
		if a != nil {
			got = len(a.overwrites)
		}

		if got != want {
			t.Errorf("%s: %d overwriting appends, want %d", name, got, want)
		}
	}
}

// TestFollowsOnlyCheckedGroups covers a function whose slices are none of the checked ones.
func TestFollowsOnlyCheckedGroups(t *testing.T) {
	if a := newAliasing(buildSource(t, overwritesSrc).Func("prefix"), func(ssa.Value) bool { return false }); a != nil {
		t.Fatalf("followed %d slices, want none", len(a.bases))
	}
}

// TestTellsUsesFromMeasures checks that len, cap, and a comparison with nil touch no element,
// and that returning the slice does.
func TestTellsUsesFromMeasures(t *testing.T) {
	fn := buildSource(t, overwritesSrc).Func("uses")
	a := newAliasing(fn, func(ssa.Value) bool { return true })

	for _, call := range instrsOf[*ssa.Call](fn) {
		if b, ok := call.Call.Value.(*ssa.Builtin); ok && b.Name() != appendBuiltin && a.uses(call) {
			t.Errorf("%s counted as a use", call)
		}
	}

	for _, bin := range instrsOf[*ssa.BinOp](fn) {
		if got, want := a.uses(bin), bin.Op != token.EQL; got != want {
			t.Errorf("%s counted as a use: %v, want %v", bin, got, want)
		}
	}

	if ret := instrOf[*ssa.Return](t, fn); !a.uses(ret) {
		t.Errorf("%s not counted as a use", ret)
	}
}

// buildSource builds the SSA of the package src declares.
func buildSource(t *testing.T, src string) *ssa.Package {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "p.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}

	pkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset,
		types.NewPackage("p", "p"), []*ast.File{file}, ssa.BuilderMode(0))
	if err != nil {
		t.Fatal(err)
	}

	return pkg
}
