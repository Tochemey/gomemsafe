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

package locks

import (
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// identitySrc has the values the identity helpers tell apart.
const identitySrc = `package p

import "sync"

type T struct {
	mu sync.Mutex
	n  int
}

type locker struct{ l sync.Locker }

func use(...any) {}

func values(t *T, ts []*T, l *locker) (int, *T) {
	fresh := new(T)
	fresh.n = 1
	var arr [2]T
	arr[0].n = 2
	use(&t.n)
	t.mu.Lock()
	l.l.Lock()
	ts[1].mu.Unlock()
	return t.n, fresh
}

func results() (int, *T) {
	n, t := values(nil, nil, nil)
	return n, t
}
`

func TestKeyPathsAndSteps(t *testing.T) {
	k := key{global: "p.g"}.field(2).index(7).field(0)

	if k.path != "2.[7].0" || !slices.Equal(k.steps(), []string{"2", "[7]", "0"}) {
		t.Fatalf("path %q, steps %v", k.path, k.steps())
	}

	if (key{}).steps() != nil || (key{}).extend([]int{1, 2}).path != "1.2" || (key{}).index(3).path != "[3]" {
		t.Fatal("an empty key's path, extension, or index is wrong")
	}
}

func TestConstKeys(t *testing.T) {
	zero := ssa.NewConst(constant.MakeInt64(0), types.Typ[types.Int])
	other := ssa.NewConst(constant.MakeInt64(0), types.Typ[types.Int])

	if constKey(zero) != constKey(other) || constKey(zero).global != constPrefix+"0" {
		t.Fatalf("two zero constants have keys %v and %v", constKey(zero), constKey(other))
	}

	if nilKey := constKey(ssa.NewConst(nil, types.NewPointer(types.Typ[types.Int]))); nilKey.global != constPrefix+"nil" {
		t.Fatalf("nil constant has key %v", nilKey)
	}
}

// TestIdentityHelpersOnSSA checks fresh, isWrite, lockMethod, resultOf, and fieldOf on the
// instructions of the fixture.
func TestIdentityHelpersOnSSA(t *testing.T) {
	pkg := buildSSA(t, identitySrc)
	fn := pkg.Func("values")

	var (
		freshFields, paramFields, writes, reads int
		lockNames                               []string
	)

	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			switch x := instr.(type) {
			case *ssa.FieldAddr:
				if fresh(x) {
					freshFields++
				} else {
					paramFields++
				}

				if isWrite(x) {
					writes++
				} else {
					reads++
				}
			case *ssa.Call:
				if receiver, name, ok := lockMethod(&x.Call); ok {
					lockNames = append(lockNames, name+"/"+receiver.Type().String())
				}
			}
		}
	}

	// Fresh: fresh.n, arr[0].n (through an IndexAddr of an Alloc). Parameters: &t.n, t.mu,
	// l.l, ts[1].mu, t.n at the return.
	if freshFields != 2 || paramFields != 5 {
		t.Errorf("%d fresh and %d parameter field accesses, want 2 and 5", freshFields, paramFields)
	}

	// Writes: fresh.n, arr[0].n. Reads: &t.n passed, t.mu, l.l, ts[1].mu, t.n loaded.
	if writes != 2 || reads != 5 {
		t.Errorf("%d writes and %d reads, want 2 and 5", writes, reads)
	}

	if want := []string{"Lock/*sync.Mutex", "Lock/sync.Locker", "Unlock/*sync.Mutex"}; !slices.Equal(lockNames, want) {
		t.Errorf("lock methods %v, want %v", lockNames, want)
	}

	call := instrOf[*ssa.Call](t, pkg.Func("results"))
	if _, ok := resultOf(call, 2); ok {
		t.Error("resultOf found a third result")
	}

	if v, ok := resultOf(call, 1); !ok || v.Type().String() != "*p.T" {
		t.Errorf("resultOf(call, 1) = %v, %v", v, ok)
	}

	if _, ok := resultOf(nil, 0); ok {
		t.Error("resultOf(nil) found a result")
	}

	single := instrOf[*ssa.Call](t, fn)
	if v, ok := resultOf(single, 0); ok && v != ssa.Value(single) {
		t.Errorf("resultOf(use(...), 0) = %v, %v; a call of use has no result", v, ok)
	}

	if _, ok := fieldOf(types.Typ[types.Int], 0); ok {
		t.Error("fieldOf(int) found a field")
	}

	if f, ok := fieldOf(pkg.Pkg.Scope().Lookup("T").Type(), 5); ok {
		t.Errorf("fieldOf(T, 5) = %v, out of range", f)
	}
}

// buildSSA parses src and builds its SSA form, so that the helpers can be driven directly.
func buildSSA(t *testing.T, src string) *ssa.Package {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}

	pkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset, types.NewPackage("p", "p"),
		[]*ast.File{file}, ssa.BuilderMode(0))
	if err != nil {
		t.Fatal(err)
	}

	return pkg
}

// instrOf returns the first instruction of type T in fn.
func instrOf[T ssa.Instruction](t *testing.T, fn *ssa.Function) T {
	t.Helper()

	var found []T

	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			if x, ok := instr.(T); ok {
				found = append(found, x)
			}
		}
	}

	if len(found) == 0 {
		t.Fatalf("%s has no %T", fn.Name(), *new(T))
	}

	return found[0]
}
