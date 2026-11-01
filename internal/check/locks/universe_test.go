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
	"go/types"
	"testing"

	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/directive"
)

// literalsSrc has function literals used every way the analysis tells apart: called directly,
// deferred, passed on, started as a goroutine, and stored into a variable; and local variables
// that are tracked, captured by inlined literals only, or not: used by address, or captured by
// a literal that passes the variable's address on.
const literalsSrc = `package p

import "sync"

type T struct {
	mu sync.Mutex
	n  int
}

type holder struct{ ref **T }

func run(f func()) { f() }

func use(...any) {}

func literals(t *T, cond bool, h *holder) {
	p := t
	r := t
	called := func() { p.n = 1 }
	called()
	defer func() { p.n = 2 }()
	run(func() { r.n = 3 })
	go func() { r.n = 4 }()
	stored := func() { r.n = 5 }
	alt := func() { r.n = 6 }
	if cond {
		stored = alt
	}
	stored()
	var q *T
	run(func() { q = t })
	_ = q
	s := t
	h.ref = &s
	a := t
	func() { use(&a) }()
}
`

// noFacts is a summary with no annotations, for driving the universe directly.
type noFacts struct{}

func (noFacts) GuardsOf(types.Object) []directive.Guard                    { return nil }
func (noFacts) LockContractOf(*types.Func) (*directive.LockContract, bool) { return nil, false }
func (noFacts) HasLockFacts(types.Object) bool                             { return false }

// TestLiteralsAreInlinedOrEscape checks which literals of the fixture are analysed in place:
// the called and deferred ones, with their free variables bound to the caller's values, and
// not the passed, started, or stored ones. The variable only inlined literals capture is
// tracked; the one an escaping literal stores to, the one whose address is stored, and the one
// an inlined literal passes on by address are not.
func TestLiteralsAreInlinedOrEscape(t *testing.T) {
	pkg := buildSSA(t, literalsSrc)
	fn := pkg.Func("literals")
	u := newUniverse(fn, nil, &checker{sum: noFacts{}})

	var inlined, escaped int

	for _, lit := range fn.AnonFuncs {
		if u.escapes(lit) {
			escaped++
		} else {
			inlined++
		}
	}

	if inlined != 3 || escaped != 5 {
		t.Fatalf("%d literals inlined and %d escaped, want 3 and 5", inlined, escaped)
	}

	frames := 0

	for site, child := range u.inlined {
		frames++

		if child.parent != u.root || site.f != u.root || len(child.bindings) != len(child.fn.FreeVars) {
			t.Errorf("frame of %s at %s is wrong: %+v", child.fn.Name(), site.instr, child)
		}
	}

	if frames != 3 {
		t.Fatalf("%d inline frames, want 3", frames)
	}

	tracked := 0

	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			if alloc, ok := instr.(*ssa.Alloc); ok && u.tracked[alloc] {
				tracked++

				if alloc.Comment != "p" {
					t.Errorf("tracked %s, want only p", alloc.Comment)
				}
			}
		}
	}

	if tracked != 1 {
		t.Fatalf("%d tracked variables, want p alone", tracked)
	}

	// Every tracked store pairs with every load, and the fact indexes cover them all.
	if len(u.holds) == 0 || len(u.is) == 0 || u.n != 4*len(u.locks)+2*len(u.defers)+len(u.holds)+len(u.is) {
		t.Fatalf("%d holds, %d is, %d facts for %d locks and %d defers", len(u.holds), len(u.is), u.n, len(u.locks), len(u.defers))
	}
}
