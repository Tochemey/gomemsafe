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
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/tochemey/gomemsafe/internal/effect"
	"github.com/tochemey/gomemsafe/internal/flow"
)

// movesSrc declares Frame and Msg, which fakeContracts treats as owned, consume, which it treats
// as consuming its argument, and the functions under test.
const movesSrc = `package p

type Frame struct {
	data []byte
	next *Frame
}

type Msg struct {
	f      *Frame
	byID   map[int]*Frame
	frames []*Frame
}

type plain struct{ f *Frame }

var global Frame

func consume(f *Frame) {}

func use(*Frame) {}

func (f *Frame) close(n int) {}

func sel(out chan *Frame, done chan struct{}, f *Frame) {
	select {
	case out <- f:
	case x, ok := <-done:
		_, _ = x, ok
	}
	use(f)
}

func merged(c bool, a, b *Frame) *Frame {
	x := a
	if c {
		x = b
	}
	return x
}

func loads(out chan Frame, p *Frame) (Frame, Frame, Frame) {
	var f Frame
	q := &f
	_ = q
	a := f
	out <- a
	b := f
	f = Frame{}
	out <- b
	c := *p
	out <- c
	return a, b, c
}

func owners(m *Msg, pl *plain, f *Frame) {
	m.f = f
	m.byID[0] = f
	m.frames[0] = f
	pl.f = f
	f.next = f
}

func laterInSource(out chan *Frame, f *Frame) {
	goto send
used:
	use(f)
	return
send:
	out <- f
	goto used
}
`

// fakeContracts treats Frame and Msg as owned, and consume as consuming its argument.
type fakeContracts struct{}

// TestLastMoveFallsBackToTheFirst covers a use with no move before it in the source, reached
// after a move through a jump back.
func TestLastMoveFallsBackToTheFirst(t *testing.T) {
	fn := buildMoves(t).Func("laterInSource")
	m := newMoves(fn, fakeContracts{})

	if mv := m.lastMove(m.classOf[fn.Params[1]], token.NoPos); mv.kind != sentOnChannel {
		t.Fatalf("move %+v, want the send", mv)
	}
}

// TestPhiIgnoresUnreachableEdges covers a phi with an edge the solver never reached: the
// phi owns its value when every edge it was reached on does.
func TestPhiIgnoresUnreachableEdges(t *testing.T) {
	fn := buildMoves(t).Func("merged")
	m := newMoves(fn, fakeContracts{})
	phi := instrOf[*ssa.Phi](t, fn)

	var owned, state flow.Bitset

	owned.Reset(m.N())
	owned.Fill()
	state.Reset(m.N())

	m.Phi(phi, func(i int) *flow.Bitset {
		if i == 0 {
			return nil
		}

		return &owned
	}, &state)

	if !state.Has(m.classOf[phi]) {
		t.Fatal("the phi is not owned, though its only reached edge is")
	}
}

func TestReferrersOfAGlobalAreNone(t *testing.T) {
	if refs := referrersOf(buildMoves(t).Var("global")); refs != nil {
		t.Fatalf("referrers = %v, want none", refs)
	}
}

func TestIsMadeOnlyByTheFunction(t *testing.T) {
	pkg := buildMoves(t)
	fn := pkg.Func("sel")

	for _, tc := range []struct {
		v    ssa.Value
		want bool
	}{
		{fn.Params[0], true},
		{instrOf[*ssa.Select](t, fn), true},
		{pkg.Var("global"), false},
		{ssa.NewConst(constant.MakeInt64(1), types.Typ[types.Int]), false},
	} {
		if got := isMade(tc.v); got != tc.want {
			t.Errorf("isMade(%v) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

func (fakeContracts) Owned(t types.Type) bool {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}

	named, ok := t.(*types.Named)

	return ok && (named.Obj().Name() == "Frame" || named.Obj().Name() == "Msg")
}

func (fakeContracts) Consumes(fn *types.Func) effect.ArgSet {
	if fn.Name() == "consume" {
		return 1
	}

	return 0
}

// buildMoves builds movesSrc into package p.
func buildMoves(t *testing.T) *ssa.Package {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "p.go", movesSrc, parser.SkipObjectResolution)
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

// instrOf returns fn's first instruction of type T.
func instrOf[T ssa.Instruction](t *testing.T, fn *ssa.Function) T {
	t.Helper()

	all := instrsOf[T](fn)
	if len(all) == 0 {
		t.Fatalf("%s has no %T", fn, *new(T))
	}

	return all[0]
}

// instrsOf returns fn's instructions of type T, in block order.
func instrsOf[T ssa.Instruction](fn *ssa.Function) []T {
	var out []T

	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			if x, ok := instr.(T); ok {
				out = append(out, x)
			}
		}
	}

	return out
}
