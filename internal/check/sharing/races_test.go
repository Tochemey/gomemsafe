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

package sharing

import (
	"testing"

	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/flow"
)

const racesSrc = `package p

type inner struct{ a, b int }

type outer struct {
	in inner
	c  int
}

func sink(*outer) {}

func fields() {
	var o outer
	o.in.a = 1
	_ = o.in.b
	o.c++
	_ = o
	sink(&o)
	go func() { _ = o }()
}

func starts(f func()) {
	x, y := 0, 0
	go f()
	go func() { _ = y }()
	go func() { x = 1 }()
	go func() { x = 2 }()
	_ = x
}

func fromLiteral() {
	x := 0
	func() {
		go func() { x = 1 }()
		_ = x
	}()
}
`

// TestAccessesOfFollowsFields covers loads and stores of a struct variable and of its fields,
// nested ones included; passing its address on is no access.
func TestAccessesOfFollowsFields(t *testing.T) {
	fn := buildSSA(t, racesSrc).Func("fields")
	o := instrOf[*ssa.Alloc](t, fn)

	type want struct {
		path  []int
		write bool
	}

	wants := []want{{[]int{0, 0}, true}, {[]int{0, 1}, false}, {[]int{1}, false}, {[]int{1}, true}, {nil, false}}

	got := accessesOf(o)
	if len(got) != len(wants) {
		t.Fatalf("got %d accesses, want %d: %+v", len(got), len(wants), got)
	}

	for _, w := range wants {
		found := false

		for _, a := range got {
			if a.v == o && a.write == w.write && len(a.path) == len(w.path) && overlaps(a.path, w.path) {
				found = true
			}
		}

		if !found {
			t.Errorf("no access with path %v and write %v in %+v", w.path, w.write, got)
		}
	}
}

func TestOverlaps(t *testing.T) {
	for _, tc := range []struct {
		a, b []int
		want bool
	}{
		{nil, nil, true},
		{nil, []int{1}, true},
		{[]int{0, 1}, []int{0}, true},
		{[]int{0, 1}, []int{0, 2}, false},
		{[]int{1}, []int{0}, false},
	} {
		if got := overlaps(tc.a, tc.b); got != tc.want {
			t.Errorf("overlaps(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestConflictNeedsAWriteToOverlappingMemory(t *testing.T) {
	read := access{path: []int{0}}
	write := access{path: []int{0}, write: true}
	otherField := access{path: []int{1}, write: true}

	p := pair{racing: []access{read}}
	if _, ok := p.conflict(read); ok {
		t.Error("two reads conflict")
	}

	if other, ok := p.conflict(write); !ok || other.write {
		t.Error("a write does not conflict with the goroutine's read")
	}

	if _, ok := p.conflict(otherField); ok {
		t.Error("a write of another field conflicts")
	}
}

// TestNewRacesPairsCapturedVariables covers a go statement of a function value, which captures
// nothing, a goroutine that only reads, and two goroutines that write one variable, whose
// accesses in the starting function are collected once.
func TestNewRacesPairsCapturedVariables(t *testing.T) {
	var eng flow.Engine

	fn := buildSSA(t, racesSrc).Func("starts")
	r := newRaces(fn, newSyncer(stubSyncs), &eng)

	if r.N() != 3 || len(r.byVar) != 2 {
		t.Fatalf("%d pairs over %d variables, want 3 over 2", r.N(), len(r.byVar))
	}

	gos := instrsOf[*ssa.Go](fn)
	if len(r.byStart[gos[0]]) != 0 {
		t.Error("a go statement of a function value has pairs")
	}

	var reads int

	for _, accesses := range r.accesses {
		for _, a := range accesses {
			if !a.write {
				reads++
			}
		}
	}

	if reads != 1 {
		t.Errorf("collected %d reads of x, want 1", reads)
	}
}

// TestTransferStartsAndResetsPairs replays the problem by hand: the first instruction holds every
// pair, a go statement starts its pairs' goroutine, making the variable again gives a variable no
// goroutine has, and synchronising holds every pair again.
func TestTransferStartsAndResetsPairs(t *testing.T) {
	var eng flow.Engine

	fn := buildSSA(t, racesSrc).Func("starts")
	r := newRaces(fn, newSyncer(stubSyncs), &eng)

	var state flow.Bitset

	state.Reset(r.N())
	r.Transfer(fn.Blocks[0].Instrs[0], &state)

	if state.Len() != r.N() {
		t.Fatalf("after the first instruction %d pairs hold, want all %d", state.Len(), r.N())
	}

	last := instrsOf[*ssa.Go](fn)[3]
	r.Transfer(last, &state)

	if state.Len() != r.N()-1 {
		t.Fatalf("after the last go statement %d pairs hold, want %d", state.Len(), r.N()-1)
	}

	k := r.byStart[last][0]
	r.Transfer(r.pairs[k].v.(*ssa.Alloc), &state)

	if !state.Has(k) {
		t.Error("making the variable again does not hold its pair")
	}

	state.Clear(k)
	r.Transfer(&ssa.RunDefers{}, &state)

	if state.Len() != r.N() {
		t.Error("synchronising does not hold every pair")
	}

	r.Phi(nil, nil, &state)
}

// TestNewRacesPairsAFreeVariable covers a function literal that starts a goroutine capturing a
// variable of its enclosing function: the pair's variable is the literal's free variable.
func TestNewRacesPairsAFreeVariable(t *testing.T) {
	var eng flow.Engine

	lit := buildSSA(t, racesSrc).Func("fromLiteral").AnonFuncs[0]

	r := newRaces(lit, newSyncer(stubSyncs), &eng)
	if r.N() != 1 {
		t.Fatalf("%d pairs, want 1", r.N())
	}

	if _, ok := r.pairs[0].v.(*ssa.FreeVar); !ok {
		t.Fatalf("the pair's variable is a %T, want an *ssa.FreeVar", r.pairs[0].v)
	}
}
