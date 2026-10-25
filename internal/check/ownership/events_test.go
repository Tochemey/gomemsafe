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
	"go/constant"
	"go/types"
	"testing"

	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/invalidation"
)

func TestCaseEdgeIsTheBranchIntoTheCase(t *testing.T) {
	sel := instrOf[*ssa.Select](t, buildMoves(t).Func("sel"))

	e, ok := caseEdge(sel, 0)
	if !ok {
		t.Fatal("no edge into case 0")
	}

	branch, isIf := e.from.Instrs[len(e.from.Instrs)-1].(*ssa.If)
	if !isIf || e.to != branch.Block().Succs[0] {
		t.Fatalf("edge %d -> %d, want the true branch of an If", e.from.Index, e.to.Index)
	}

	if _, ok := caseEdge(sel, 7); ok {
		t.Fatal("found an edge into a case the select does not have")
	}
}

// TestSelectWithoutItsBranchMovesAtTheSelect covers a select whose index nothing compares: its
// send moves at the select itself. A receive, and a send of a value that is not owned, move
// nothing.
func TestSelectWithoutItsBranchMovesAtTheSelect(t *testing.T) {
	fn := buildMoves(t).Func("sel")
	m := newMoves(fn, fakeContracts{})
	sel := &ssa.Select{States: []*ssa.SelectState{
		{Dir: types.SendOnly, Send: fn.Params[2]},
		{Dir: types.RecvOnly},
		{Dir: types.SendOnly, Send: fn.Params[1]},
	}}

	m.selectMoves(sel, fakeContracts{})

	if len(m.at[sel]) != 1 {
		t.Fatalf("%d moves at the select, want 1", len(m.at[sel]))
	}
}

// TestLoadedFromIsTheVariableJustLoaded covers the three sends of loads: the first sends the
// variable's content, the second an old content the variable no longer holds, and the third a
// value loaded through a pointer, which is no variable.
func TestLoadedFromIsTheVariableJustLoaded(t *testing.T) {
	sends := instrsOf[*ssa.Send](buildMoves(t).Func("loads"))
	if len(sends) != 3 {
		t.Fatalf("%d sends, want 3", len(sends))
	}

	for i, want := range []bool{true, false, false} {
		if _, ok := loadedFrom(invalidation.Base(sends[i].X), sends[i]); ok != want {
			t.Errorf("send %d: loaded from a variable = %v, want %v", i, ok, want)
		}
	}

	if _, ok := loadedFrom(sends[0].Chan, sends[0]); ok {
		t.Error("a parameter is loaded from a variable")
	}
}

// TestIsOwnerFollowsFieldsElementsAndLoads covers the stores of owners: into a field, a map
// entry, and a slice element of an owned value, but not into a value that is not owned, nor
// into the stored value itself.
func TestIsOwnerFollowsFieldsElementsAndLoads(t *testing.T) {
	fn := buildMoves(t).Func("owners")
	f := fn.Params[2]

	var got []bool

	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			switch x := instr.(type) {
			case *ssa.Store:
				got = append(got, isOwner(x.Addr, f, fakeContracts{}))
			case *ssa.MapUpdate:
				got = append(got, isOwner(x.Map, f, fakeContracts{}))
			}
		}
	}

	want := []bool{true, true, true, false, false}
	if len(got) != len(want) {
		t.Fatalf("owners = %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("store %d: owner = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestParamAtCountsTheReceiverFirst(t *testing.T) {
	obj := buildMoves(t).Type("Frame").Type().(*types.Named).Method(0)

	if got := paramAt(obj, 0); got.Name() != "f" {
		t.Errorf("argument 0 is %s, want the receiver f", got.Name())
	}

	if got := paramAt(obj, 1); got.Name() != "n" {
		t.Errorf("argument 1 is %s, want n", got.Name())
	}
}

func TestIsConstIntIsAnIntegerConstant(t *testing.T) {
	for _, tc := range []struct {
		v    ssa.Value
		want bool
	}{
		{ssa.NewConst(constant.MakeInt64(2), types.Typ[types.Int]), true},
		{ssa.NewConst(constant.MakeInt64(3), types.Typ[types.Int]), false},
		{ssa.NewConst(constant.MakeString("2"), types.Typ[types.String]), false},
		{ssa.NewConst(nil, types.NewPointer(types.Typ[types.Int])), false},
		{&ssa.Parameter{}, false},
	} {
		if got := isConstInt(tc.v, 2); got != tc.want {
			t.Errorf("isConstInt(%v, 2) = %v, want %v", tc.v, got, tc.want)
		}
	}
}
