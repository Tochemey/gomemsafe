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
	"go/types"
	"slices"

	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/flow"
)

// access is a load or a store of a variable, or of a field of it, or an operation on the map
// such a load yields.
type access struct {
	instr ssa.Instruction
	// pos is where the access names what it accesses: the variable, or for an operation on its
	// map, the variable the map is loaded from, rather than the operator SSA places it at.
	pos token.Pos
	// v is the variable's address in the function that makes the access.
	v ssa.Value
	// path lists the field indexes from the variable to what is loaded or stored: empty for the
	// whole variable.
	path  []int
	write bool
	// inMap marks an operation on the map the variable holds, rather than on the variable.
	inMap bool
}

// pair is a variable of a function that a goroutine it starts captures, with the goroutine's
// accesses to that variable, or to the map it holds, that no synchronisation precedes on every
// path.
type pair struct {
	start *ssa.Go
	// v is the variable's address in the starting function: an *ssa.Alloc, or an *ssa.FreeVar
	// when that function is itself a function literal.
	v      ssa.Value
	racing []access
	// inMap marks a pair of accesses to the map the variable holds (MS402), rather than to the
	// variable (MS401).
	inMap bool
}

// races is the dataflow problem of a function that starts goroutines, with one fact per pair:
// fact k holds where, on every path, pair k's goroutine has not been started since its variable
// was made, or the function synchronised after starting it. For a pair of map accesses, it holds
// too where the function stored the variable since, so that it holds another map than the one
// the goroutine may use. An access where the fact does not hold may race with the goroutine.
type races struct {
	fn      *ssa.Function
	sync    *syncer
	pairs   []pair
	byStart map[*ssa.Go][]int
	byVar   map[ssa.Value][]int
	// accesses holds the function's accesses to the variables of its pairs, by instruction.
	accesses map[ssa.Instruction][]access
	// replaces holds, by store of a variable, the pairs of map accesses whose map it replaces.
	replaces map[ssa.Instruction][]int
}

// N implements flow.Problem.
func (r *races) N() int { return len(r.pairs) }

// Phi implements flow.Problem. A phi starts no goroutine and synchronises nothing; the engine
// meets the incoming states.
func (*races) Phi(*ssa.Phi, func(int) *flow.Bitset, *flow.Bitset) {}

// Transfer implements flow.Problem. At the function's first instruction no goroutine runs yet.
// A go statement starts its pairs' goroutine; making a variable again, as a loop's body does
// with the variables declared in it, gives a variable no goroutine has; storing a variable gives
// it a map no goroutine has; and synchronising may order every goroutine started so far before
// what follows.
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
	case *ssa.Store:
		for _, k := range r.replaces[x] {
			state.Set(k)
		}
	default:
		if r.sync.synchronizes(instr) {
			r.setAll(state)
		}
	}
}

// newRaces returns the problem of fn: a pair for each variable of fn that a goroutine fn starts
// captures and may access unsynchronised, and another for the map it holds, and fn's accesses
// to those variables. The goroutines' bodies are solved with eng, so no solution of eng may be
// in use.
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
	r.replaces = make(map[ssa.Instruction][]int)

	for v := range r.byVar {
		for _, a := range accessesOf(v) {
			r.accesses[a.instr] = append(r.accesses[a.instr], a)

			if a.write && !a.inMap {
				r.addReplaced(a)
			}
		}
	}

	return r
}

// addPairs adds a pair for each variable that the function literal g starts captures and
// accesses unsynchronised, and one for each map such a variable holds that it operates on
// unsynchronised. A map operation is dropped where the goroutine also stores the variable
// unsynchronised: which map it operates on is then unknown, and the store races with the
// starting function's accesses itself.
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

		var onVariable, onMap []access

		for _, a := range racing[i] {
			switch {
			case !a.inMap:
				onVariable = append(onVariable, a)
			case !replaced(racing[i], a):
				onMap = append(onMap, a)
			}
		}

		r.addPair(pair{start: g, v: binding, racing: onVariable})
		r.addPair(pair{start: g, v: binding, racing: onMap, inMap: true})
	}
}

// addPair adds p, unless the goroutine makes no access that may race.
func (r *races) addPair(p pair) {
	if len(p.racing) == 0 {
		return
	}

	k := len(r.pairs)
	r.pairs = append(r.pairs, p)
	r.byStart[p.start] = append(r.byStart[p.start], k)
	r.byVar[p.v] = append(r.byVar[p.v], k)
}

// addReplaced records the pairs of map accesses whose map the store s of a variable replaces:
// those of the variable whose map the store overwrites, or the struct holding it.
func (r *races) addReplaced(s access) {
	for _, k := range r.byVar[s.v] {
		if r.pairs[k].inMap && slices.ContainsFunc(r.pairs[k].racing, func(m access) bool { return replaces(s, m) }) {
			r.replaces[s.instr] = append(r.replaces[s.instr], k)
		}
	}
}

func (r *races) setAll(state *flow.Bitset) {
	for k := range r.pairs {
		state.Set(k)
	}
}

// conflict returns an access of the goroutine that conflicts with a: both are to the variable or
// both to the map it holds, one of the two writes, and they reach the same part of the variable.
func (p pair) conflict(a access) (access, bool) {
	for _, other := range p.racing {
		if a.inMap == other.inMap && (a.write || other.write) && overlaps(a.path, other.path) {
			return other, true
		}
	}

	return access{}, false
}

// accessesOf returns the accesses to the variable at the address v, in v's function: the loads
// and stores of the variable itself and of the fields of a struct variable, and the operations on
// the map such a load yields.
func accessesOf(v ssa.Value) []access {
	var out []access

	stores := make(map[ssa.Instruction]bool)

	var walk func(addr ssa.Value, path []int)

	walk = func(addr ssa.Value, path []int) {
		for _, ref := range *addr.Referrers() {
			switch x := ref.(type) {
			case *ssa.Store:
				if x.Addr == addr {
					out = append(out, access{instr: x, pos: x.Pos(), v: v, path: path, write: true})
					stores[x] = true
				}
			case *ssa.UnOp:
				if x.Op == token.MUL {
					out = append(out, access{instr: x, pos: x.Pos(), v: v, path: path})
				}
			case *ssa.FieldAddr:
				walk(x, append(slices.Clip(path), x.Field))
			}
		}
	}

	walk(v, nil)

	var onMaps []access

	for _, a := range out {
		if load, ok := a.instr.(*ssa.UnOp); ok {
			onMaps = append(onMaps, mapAccesses(load, a, stores)...)
		}
	}

	return append(out, onMaps...)
}

// mapAccesses returns the operations on the map that load, a load access of a variable, yields:
// writes into it with an assignment, delete, or clear, and reads from it with an index, range, or
// len. Only an operation in load's block with no store of the variable between them counts:
// after one, the variable may hold another map than the one the operation is on.
func mapAccesses(load *ssa.UnOp, a access, stores map[ssa.Instruction]bool) []access {
	if _, isMap := load.Type().Underlying().(*types.Map); !isMap {
		return nil
	}

	var out []access

	for _, ref := range *load.Referrers() {
		write, ok := mapOperation(ref, load)
		if ok && !storedBetween(load, ref, stores) {
			out = append(out, access{instr: ref, pos: load.Pos(), v: a.v, path: a.path, write: write, inMap: true})
		}
	}

	return out
}

// mapOperation reports whether instr operates on the map m, and whether it writes into it.
func mapOperation(instr ssa.Instruction, m ssa.Value) (write, ok bool) {
	switch x := instr.(type) {
	case *ssa.MapUpdate:
		return true, x.Map == m
	case *ssa.Lookup:
		return false, x.X == m
	case *ssa.Range:
		return false, x.X == m
	case *ssa.Call:
		builtin, isBuiltin := x.Call.Value.(*ssa.Builtin)
		if !isBuiltin || len(x.Call.Args) == 0 || x.Call.Args[0] != m {
			return false, false
		}

		switch builtin.Name() {
		case "delete", "clear":
			return true, true
		case "len":
			return false, true
		}
	}

	return false, false
}

// storedBetween reports whether op, which uses the value load yields, is outside load's block, or
// a store among stores lies between them in it. Within one block, a use follows its value.
func storedBetween(load, op ssa.Instruction, stores map[ssa.Instruction]bool) bool {
	if op.Block() != load.Block() {
		return true
	}

	instrs := load.Block().Instrs
	between := instrs[slices.Index(instrs, load)+1 : slices.Index(instrs, op)]

	return slices.ContainsFunc(between, func(instr ssa.Instruction) bool { return stores[instr] })
}

// replaced reports whether some store among accesses replaces the map the access a operates on.
func replaced(accesses []access, a access) bool {
	return slices.ContainsFunc(accesses, func(s access) bool { return replaces(s, a) })
}

// replaces reports whether s is a store of the variable that replaces the map the access m
// operates on: it stores the field path m loads, or a part of the variable that holds it.
func replaces(s, m access) bool {
	return s.write && !s.inMap && m.inMap && len(s.path) <= len(m.path) && overlaps(s.path, m.path)
}

// overlaps reports whether two field paths into one variable reach overlapping memory: one is a
// prefix of the other.
func overlaps(a, b []int) bool {
	n := min(len(a), len(b))
	return slices.Equal(a[:n], b[:n])
}
