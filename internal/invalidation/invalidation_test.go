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

package invalidation_test

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/tochemey/gomemsafe/internal/effect"
	"github.com/tochemey/gomemsafe/internal/flow"
	"github.com/tochemey/gomemsafe/internal/invalidation"
)

// src declares release, which the test lookup treats as invalidating its first argument,
// releaser.free, which it treats as invalidating its receiver, and the functions under test.
const src = `package p

type box struct{ v *int }

func release(x any) {}
func use(x *int) {}

func straight(x *int) { release(x); use(x) }

func viaInterface(x *int) { var i any = x; release(i); use(i.(*int)) }

func onePath(c bool, x *int) { if c { release(x) }; use(x) }

func bothPhi(c bool, a, b *int) {
	var p *int
	if c {
		release(a)
		p = a
	} else {
		release(b)
		p = b
	}
	use(p)
}

func onePhi(c bool, a, b *int) {
	var p *int
	if c {
		release(a)
		p = a
	} else {
		p = b
	}
	use(p)
}

func nilCheck(x *int) bool { release(x); return x == nil }

func constArg() { release(nil) }

func noCalls(x *int) { use(x) }

func commaOk(i any) { x, ok := i.(*int); release(x); if ok { use(x) } }

func neverReturns(x *int) { release(x); for {} }

func releasesAll(x *int, y int) { release(x) }

func resliced(s []byte) byte { release(s[:0]); return s[0] }

func copied(n int, s string, b box) { release(n); release(s); release(b) }

func generic[T any](v T) { release(v) }

type releaser struct{}

func (releaser) free(x *int) {}

func methodValue(r releaser, x *int) { f := r.free; f(x); use(x) }
`

func TestUsesFindUseAfterInvalidation(t *testing.T) {
	pkg := build(t)

	for _, name := range []string{"straight", "viaInterface", "bothPhi", "commaOk", "resliced"} {
		a := analyze(t, pkg, name)

		uses := a.Uses()
		if len(uses) != 1 {
			t.Errorf("%s: %d uses, want 1", name, len(uses))
			continue
		}

		if uses[0].Site.Callee.Name() != "release" || uses[0].Arg != 0 {
			t.Errorf("%s: use attributed to %s argument %d", name, uses[0].Site.Callee.Name(), uses[0].Arg)
		}
	}
}

func TestUsesIgnoreNonUses(t *testing.T) {
	pkg := build(t)

	for _, name := range []string{"onePath", "onePhi", "nilCheck"} {
		if uses := analyze(t, pkg, name).Uses(); len(uses) != 0 {
			t.Errorf("%s: %d uses, want 0", name, len(uses))
		}
	}
}

func TestAnalyzeReturnsNilWithoutSites(t *testing.T) {
	pkg := build(t)

	for _, name := range []string{"constArg", "noCalls"} {
		var eng flow.Engine

		if a := invalidation.Analyze(pkg.Func(name), lookup, &eng); a != nil {
			t.Errorf("%s: got an analysis, want nil", name)
		}
	}
}

// TestAnalyzeSkipsArgumentsThatShareNothing covers values copied by the call (an int, a
// string, a struct, a type parameter) and a method value, whose SSA arguments do not line up
// with the method's parameters.
func TestAnalyzeSkipsArgumentsThatShareNothing(t *testing.T) {
	pkg := build(t)

	for _, name := range []string{"copied", "generic", "methodValue"} {
		var eng flow.Engine

		if a := invalidation.Analyze(pkg.Func(name), lookup, &eng); a != nil {
			t.Errorf("%s: got %d sites, want none", name, len(a.Sites))
		}
	}
}

func TestAnalyzeReturnsNilPastBudget(t *testing.T) {
	eng := flow.Engine{Budget: 1}

	if a := invalidation.Analyze(build(t).Func("bothPhi"), lookup, &eng); a != nil {
		t.Fatal("got an analysis past the budget, want nil")
	}
}

func TestInvalidAtReturn(t *testing.T) {
	pkg := build(t)

	tests := []struct {
		name string
		want []bool
	}{
		{"releasesAll", []bool{true, false}},
		{"onePath", []bool{false, false}},
		{"neverReturns", []bool{false}},
	}

	for _, tt := range tests {
		got := analyze(t, pkg, tt.name).InvalidAtReturn()
		if len(got) != len(tt.want) {
			t.Fatalf("%s: %v, want %v", tt.name, got, tt.want)
		}

		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
				break
			}
		}
	}
}

func TestBaseStripsConversionsThatKeepTheMemory(t *testing.T) {
	pkg := build(t)

	for _, name := range []string{"viaInterface", "resliced"} {
		fn := pkg.Func(name)
		param := fn.Params[0]

		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				v, ok := instr.(ssa.Value)
				if !ok {
					continue
				}

				switch v.(type) {
				case *ssa.MakeInterface, *ssa.TypeAssert, *ssa.Slice:
					if got := invalidation.Base(v); got != param {
						t.Errorf("%s: Base(%s) = %s, want the parameter", name, v.Name(), got.Name())
					}
				}
			}
		}
	}
}

func lookup(fn *types.Func) effect.ArgSet {
	switch fn.Name() {
	case "release", "free":
		return 1
	}

	return 0
}

func analyze(t *testing.T, pkg *ssa.Package, name string) *invalidation.Analysis {
	t.Helper()

	var eng flow.Engine

	a := invalidation.Analyze(pkg.Func(name), lookup, &eng)
	if a == nil {
		t.Fatalf("%s: no analysis", name)
	}

	return a
}

func build(t *testing.T) *ssa.Package {
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
