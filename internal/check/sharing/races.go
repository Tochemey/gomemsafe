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
	"go/token"
	"slices"

	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/flow"
)

// access is a load or a store of a variable, or of a field of it.
type access struct {
	instr ssa.Instruction
	// v is the variable's address in the function that makes the access.
	v ssa.Value
	// path lists the field indexes from the variable to what is accessed: empty for the whole
	// variable.
	path  []int
	write bool
}

// pair is a variable of a function that a goroutine it starts captures, with the goroutine's
// accesses to that variable that no synchronisation precedes on every path.
type pair struct {
	start *ssa.Go
	// v is the variable's address in the starting function: an *ssa.Alloc, or an *ssa.FreeVar
	// when that function is itself a function literal.
	v      ssa.Value
	racing []access
}

// races is the dataflow problem of a function that starts goroutines, with one fact per pair:
// fact k holds where, on every path, pair k's goroutine has not been started since its variable
// was made, or the function synchronised after starting it. An access to the variable where the
// fact does not hold may race with the goroutine.
type races struct {
	fn      *ssa.Function
	sync    *syncer
	pairs   []pair
	byStart map[*ssa.Go][]int
	byVar   map[ssa.Value][]int
	// accesses holds the function's accesses to the variables of its pairs, by instruction.
	accesses map[ssa.Instruction][]access
}

// N implements flow.Problem.
func (r *races) N() int { return len(r.pairs) }

// Phi implements flow.Problem. A phi starts no goroutine and synchronises nothing; the engine
// meets the incoming states.
func (*races) Phi(*ssa.Phi, func(int) *flow.Bitset, *flow.Bitset) {}

// Transfer implements flow.Problem. At the function's first instruction no goroutine runs yet.
// A go statement starts its pairs' goroutine; making a variable again, as a loop's body does
// with the variables declared in it, gives a variable no goroutine has; and synchronising may
// order every goroutine started so far before what follows.
func (r *races) Transfer(instr ssa.Instruction, state *flow.Bitset) {
	if instr == r.fn.Blocks[0].Instrs[0] {
		r.setAll(state)
	}

	switch x := instr.(type) {
	case *ssa.Go:
		for _, k := range r.byStart[x] {
			state.Clear(k)
		}
	case *ssa.Alloc:
		for _, k := range r.byVar[x] {
			state.Set(k)
		}
	default:
		if r.sync.synchronizes(instr) {
			r.setAll(state)
		}
	}
}

// newRaces returns the problem of fn: a pair for each variable of fn that a goroutine fn starts
// captures and may access unsynchronised, and fn's accesses to those variables. The goroutines'
// bodies are solved with eng, so no solution of eng may be in use.
func newRaces(fn *ssa.Function, sync *syncer, eng *flow.Engine) *races {
	r := &races{fn: fn, sync: sync, byStart: make(map[*ssa.Go][]int), byVar: make(map[ssa.Value][]int)}

	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			if g, ok := instr.(*ssa.Go); ok {
				r.addPairs(g, eng)
			}
		}
	}

	if len(r.pairs) == 0 {
		return r
	}

	r.accesses = make(map[ssa.Instruction][]access)

	for v := range r.byVar {
		for _, a := range accessesOf(v) {
			r.accesses[a.instr] = append(r.accesses[a.instr], a)
		}
	}

	return r
}

// addPairs adds a pair for each variable that the function literal g starts captures and
// accesses unsynchronised.
func (r *races) addPairs(g *ssa.Go, eng *flow.Engine) {
	closure, ok := g.Call.Value.(*ssa.MakeClosure)
	if !ok {
		return
	}

	body := closure.Fn.(*ssa.Function)
	racing := r.sync.unsynchronized(body, eng)

	for i, binding := range closure.Bindings {
		switch binding.(type) {
		case *ssa.Alloc, *ssa.FreeVar:
		default:
			continue
		}

		if len(racing[i]) == 0 {
			continue
		}

		k := len(r.pairs)
		r.pairs = append(r.pairs, pair{start: g, v: binding, racing: racing[i]})
		r.byStart[g] = append(r.byStart[g], k)
		r.byVar[binding] = append(r.byVar[binding], k)
	}
}

func (r *races) setAll(state *flow.Bitset) {
	for k := range r.pairs {
		state.Set(k)
	}
}

// conflict returns an access of the goroutine that conflicts with a: one of the two writes, and
// they reach the same part of the variable.
func (p pair) conflict(a access) (access, bool) {
	for _, other := range p.racing {
		if (a.write || other.write) && overlaps(a.path, other.path) {
			return other, true
		}
	}

	return access{}, false
}

// accessesOf returns the loads and stores of the variable at the address v, in v's function: of
// the variable itself, and of the fields of a struct variable.
func accessesOf(v ssa.Value) []access {
	var out []access

	var walk func(addr ssa.Value, path []int)

	walk = func(addr ssa.Value, path []int) {
		for _, ref := range *addr.Referrers() {
			switch x := ref.(type) {
			case *ssa.Store:
				if x.Addr == addr {
					out = append(out, access{instr: x, v: v, path: path, write: true})
				}
			case *ssa.UnOp:
				if x.Op == token.MUL {
					out = append(out, access{instr: x, v: v, path: path})
				}
			case *ssa.FieldAddr:
				walk(x, append(slices.Clip(path), x.Field))
			}
		}
	}

	walk(v, nil)

	return out
}

// overlaps reports whether two field paths into one variable reach overlapping memory: one is a
// prefix of the other.
func overlaps(a, b []int) bool {
	n := min(len(a), len(b))
	return slices.Equal(a[:n], b[:n])
}
