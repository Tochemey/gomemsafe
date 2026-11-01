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
	"go/token"
	"go/types"
	"strconv"

	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/directive"
	"github.com/tochemey/gomemsafe/internal/flow"
)

// frame is one function under analysis: a declaration, a function literal analysed on its
// own, or a function literal inlined at a call of it, with its free variables and parameters
// bound to the caller's values.
type frame struct {
	fn     *ssa.Function
	parent *frame
	// bindings maps the free variables and parameters of fn to values of the parent frame.
	bindings map[ssa.Value]ssa.Value
	// contract is the declaration's own, for the outermost frame.
	contract *directive.LockContract
	// entry is the state at fn's first instruction when inlined: the caller's state at the
	// call.
	entry flow.Bitset
	// exit meets the states at fn's returns when inlined; returned says one was reached.
	exit     flow.Bitset
	returned bool
	// eng solves fn when inlined; it is kept so that its buffers are reused at each visit.
	eng flow.Engine
}

// stored is a value stored into a tracked local variable, with the frame that stored it, in
// which the value resolves.
type stored struct {
	v ssa.Value
	f *frame
}

// holdPair is the fact that a tracked local variable holds a value stored by one frame;
// isPair the fact that a load of it yielded that value. The frame matters: a literal inlined
// at two calls stores its parameter twice, bound to a different argument each time.
type holdPair struct {
	alloc *ssa.Alloc
	s     stored
}

type isPair struct {
	load *ssa.UnOp
	s    stored
}

// inlineSite is a call or defer of a function literal, in the frame that makes it.
type inlineSite struct {
	instr ssa.Instruction
	f     *frame
}

// universe is everything the facts of one analysis are about: the locks the function and its
// inlined literals may hold, their deferred calls, and the local variables whose contents are
// followed.
type universe struct {
	root *frame
	// locks lists the lock keys; each has four facts: held, held exclusively, free, and not
	// held exclusively. Held and free both unset is a lock held on some paths only; held with
	// neither exclusive nor not exclusive is a lock held in different modes on different paths.
	locks     []key
	lockIndex map[key]int
	// defers lists the deferred calls; each has two facts: pending, and not pending.
	defers     []*ssa.Defer
	deferIndex map[*ssa.Defer]int
	// tracked holds the local variables whose contents are followed: those that only loads,
	// stores, and inlined literals use, so that every store is seen.
	tracked map[*ssa.Alloc]bool
	stores  map[*ssa.Alloc][]stored
	holds   map[holdPair]int
	is      map[isPair]int
	// inlined maps each call or defer of a function literal to the literal's frame there.
	inlined map[inlineSite]*frame
	// escaped caches whether a literal is used other than by a direct call or defer.
	escaped map[*ssa.Function]bool
	// indexKeys numbers the keys of element indexes, so that a path can name one.
	indexKeys []key
	indexIDs  map[key]int
	n         int
}

// index extends k with an element whose index has the key numbered id.
func (k key) index(id int) key {
	step := "[" + strconv.Itoa(id) + "]"
	if k.path == "" {
		k.path = step
	} else {
		k.path += "." + step
	}

	return k
}

// isAlloc reports whether k is a tracked local variable itself.
func (u *universe) isAlloc(k key) (*ssa.Alloc, bool) {
	alloc, ok := k.root.(*ssa.Alloc)
	return alloc, ok && k.path == "" && u.tracked[alloc]
}

// newUniverse builds the universe of fn: it finds the frames of the literals fn inlines, the
// local variables to follow and their stores, then the lock keys every lock operation, guarded
// access, and contract may name.
func newUniverse(fn *ssa.Function, contract *directive.LockContract, c *checker) *universe {
	u := &universe{
		root: &frame{fn: fn, contract: contract}, lockIndex: make(map[key]int), deferIndex: make(map[*ssa.Defer]int),
		tracked: make(map[*ssa.Alloc]bool), stores: make(map[*ssa.Alloc][]stored), holds: make(map[holdPair]int),
		is: make(map[isPair]int), inlined: make(map[inlineSite]*frame), escaped: make(map[*ssa.Function]bool),
		indexIDs: make(map[key]int),
	}

	u.walk(u.root, u.trackAllocs)
	u.walk(u.root, u.trackStores)
	u.walk(u.root, u.trackLoads)
	u.walk(u.root, func(instr ssa.Instruction, f *frame) { u.internKeys(instr, f, c) })

	for alloc, stores := range u.stores {
		for _, s := range stores {
			u.holds[holdPair{alloc, s}] = 0
		}
	}

	u.index()

	return u
}

// walk visits every instruction of f, and of the literals inlined in it, with their frame.
func (u *universe) walk(f *frame, visit func(ssa.Instruction, *frame)) {
	for _, b := range f.fn.Blocks {
		for _, instr := range b.Instrs {
			visit(instr, f)

			if child := u.inlineAt(instr, f); child != nil {
				u.walk(child, visit)
			}
		}
	}
}

// inlineAt returns the frame of the function literal that instr, in f, calls or defers
// directly, if the literal is used nowhere else; nil otherwise.
func (u *universe) inlineAt(instr ssa.Instruction, f *frame) *frame {
	site := inlineSite{instr, f}
	if child, ok := u.inlined[site]; ok {
		return child
	}

	call, ok := instr.(ssa.CallInstruction)
	if !ok {
		return nil
	}

	if _, isGo := instr.(*ssa.Go); isGo {
		return nil
	}

	common := call.Common()
	if common.IsInvoke() {
		return nil
	}

	var (
		lit      *ssa.Function
		bindings = make(map[ssa.Value]ssa.Value)
	)

	switch callee := common.Value.(type) {
	case *ssa.Function:
		lit = callee
	case *ssa.MakeClosure:
		lit = callee.Fn.(*ssa.Function)
		for i, fv := range lit.FreeVars {
			bindings[fv] = callee.Bindings[i]
		}
	default:
		return nil
	}

	if lit.Parent() == nil || u.escapes(lit) || len(lit.Params) != len(common.Args) {
		return nil
	}

	for i, param := range lit.Params {
		bindings[param] = common.Args[i]
	}

	child := &frame{fn: lit, parent: f, bindings: bindings}
	u.inlined[site] = child

	return child
}

// escapes reports whether the literal lit is used other than as the callee of a call or a
// defer: passed on, stored, or started as a goroutine. Such a literal runs with a lock state
// the analysis cannot know, so it is analysed on its own with none.
func (u *universe) escapes(lit *ssa.Function) bool {
	if escaped, ok := u.escaped[lit]; ok {
		return escaped
	}

	escaped := false

	for _, b := range lit.Parent().Blocks {
		for _, instr := range b.Instrs {
			var ops [8]*ssa.Value

			for _, op := range instr.Operands(ops[:0]) {
				if *op == nil {
					continue
				}

				mc, isClosure := (*op).(*ssa.MakeClosure)

				switch {
				case *op == ssa.Value(lit):
					escaped = escaped || !directCall(instr, *op)
				case isClosure && mc.Fn == ssa.Value(lit):
					for _, ref := range *mc.Referrers() {
						escaped = escaped || !directCall(ref, mc)
					}
				}
			}
		}
	}

	u.escaped[lit] = escaped

	return escaped
}

// directCall reports whether instr calls or defers fn itself.
func directCall(instr ssa.Instruction, fn ssa.Value) bool {
	switch x := instr.(type) {
	case *ssa.Call:
		return x.Call.Value == fn
	case *ssa.Defer:
		return x.Call.Value == fn
	case *ssa.MakeClosure:
		return true
	}

	return false
}

// trackAllocs records the local variables whose every use is a load, a store, or a capture by
// an inlined literal, so that what they hold is known.
func (u *universe) trackAllocs(instr ssa.Instruction, _ *frame) {
	if alloc, ok := instr.(*ssa.Alloc); ok && u.local(alloc, make(map[ssa.Value]bool)) {
		u.tracked[alloc] = true
	}
}

// local reports whether every use of the address v is a load, a store, or a capture by a
// literal that is only called directly, whose free variable is used so too.
func (u *universe) local(v ssa.Value, seen map[ssa.Value]bool) bool {
	if seen[v] {
		return true
	}

	seen[v] = true

	refs := v.Referrers()
	if refs == nil {
		return false
	}

	for _, ref := range *refs {
		switch x := ref.(type) {
		case *ssa.UnOp:
			if x.Op != token.MUL {
				return false
			}
		case *ssa.Store:
			if x.Addr != v {
				return false
			}
		case *ssa.MakeClosure:
			lit := x.Fn.(*ssa.Function)
			if u.escapes(lit) {
				return false
			}

			for i, binding := range x.Bindings {
				if binding == v && !u.local(lit.FreeVars[i], seen) {
					return false
				}
			}
		default:
			return false
		}
	}

	return true
}

// trackStores records each store into a tracked local variable, and each defer.
func (u *universe) trackStores(instr ssa.Instruction, f *frame) {
	switch x := instr.(type) {
	case *ssa.Store:
		if alloc, ok := u.isAlloc(u.resolve(x.Addr, f, nil, nil)); ok {
			u.stores[alloc] = append(u.stores[alloc], stored{x.Val, f})
		}
	case *ssa.Defer:
		if _, seen := u.deferIndex[x]; !seen {
			u.deferIndex[x] = len(u.defers)
			u.defers = append(u.defers, x)
		}
	}
}

// trackLoads pairs each load of a tracked local variable with every value stored into it,
// wherever the store is: a load in a loop may yield a value stored later in the body.
func (u *universe) trackLoads(instr ssa.Instruction, f *frame) {
	load, ok := instr.(*ssa.UnOp)
	if !ok || load.Op != token.MUL {
		return
	}

	if alloc, ok := u.isAlloc(u.resolve(load.X, f, nil, nil)); ok {
		for _, s := range u.stores[alloc] {
			u.is[isPair{load, s}] = 0
		}
	}
}

// internKeys records the lock keys instr may name: through a lock method, a guarded access, a
// contract of the callee, or the frame's own contract.
func (u *universe) internKeys(instr ssa.Instruction, f *frame, c *checker) {
	if f == u.root && instr == f.fn.Blocks[0].Instrs[0] && f.contract != nil {
		for _, ref := range append(f.contract.HeldOnEntry(), f.contract.HeldOnExit()...) {
			if ref.Kind != directive.LockResult {
				u.internRef(ref, nil, nil, f)
			}
		}
	}

	switch x := instr.(type) {
	case *ssa.FieldAddr:
		u.internGuards(x.X, x.Field, f, c)
	case *ssa.Field:
		u.internGuards(x.X, x.Field, f, c)
	case *ssa.Return:
		if f == u.root && f.contract != nil {
			for _, ref := range f.contract.HeldOnExit() {
				if ref.Kind == directive.LockResult {
					u.internAll(x.Results[ref.Index], ref.Path, f)
				}
			}
		}
	case ssa.CallInstruction:
		u.internCall(x, f, c)
	}

	var ops [8]*ssa.Value

	for _, op := range instr.Operands(ops[:0]) {
		if g, ok := (*op).(*ssa.Global); ok {
			for _, guard := range c.sum.GuardsOf(g.Object()) {
				u.intern(guardKey(guard, key{}))
			}
		}
	}
}

// internGuards records the keys of the guards of the field of base.
func (u *universe) internGuards(base ssa.Value, field int, f *frame, c *checker) {
	obj, ok := fieldOf(base.Type(), field)
	if !ok || fresh(base) {
		return
	}

	for _, guard := range c.sum.GuardsOf(obj) {
		if guard.Global != "" {
			u.intern(guardKey(guard, key{}))
			continue
		}

		for _, k := range u.possible(base, f, nil) {
			u.intern(guardKey(guard, k))
		}
	}
}

// internCall records the keys a call may name: the receiver of a lock method, and the locks
// of the callee's contract.
func (u *universe) internCall(call ssa.CallInstruction, f *frame, c *checker) {
	common := call.Common()

	if receiver, _, ok := lockMethod(common); ok {
		u.internAll(receiver, nil, f)
	}

	callee, ok := common.Value.(*ssa.Function)
	if !ok {
		return
	}

	obj, ok := callee.Object().(*types.Func)
	if !ok {
		return
	}

	contract, ok := c.sum.LockContractOf(obj)
	if !ok {
		return
	}

	for _, ref := range append(append(contract.HeldOnEntry(), contract.HeldOnExit()...), contract.Excludes...) {
		u.internRef(ref, common.Args, call.Value(), f)
	}
}

// internRef records the keys the contract reference ref may name at a call with args and
// result, or, with no args, in the frame's own function.
func (u *universe) internRef(ref directive.LockRef, args []ssa.Value, result ssa.Value, f *frame) {
	switch ref.Kind {
	case directive.LockGlobal:
		u.intern(key{global: ref.Global}.extend(ref.Path))
	case directive.LockParam:
		if args == nil {
			u.internAll(f.fn.Params[ref.Index], ref.Path, f)
		} else if ref.Index < len(args) {
			u.internAll(args[ref.Index], ref.Path, f)
		}
	case directive.LockResult:
		if v, ok := resultOf(result, ref.Index); ok {
			u.internAll(v, ref.Path, f)
		}
	}
}

// internAll records every key v may resolve to, extended by path.
func (u *universe) internAll(v ssa.Value, path []int, f *frame) {
	for _, k := range u.possible(v, f, nil) {
		u.intern(k.extend(path))
	}
}

func (u *universe) intern(k key) {
	if _, ok := u.lockIndex[k]; !ok {
		u.lockIndex[k] = len(u.locks)
		u.locks = append(u.locks, k)
	}
}

// index numbers the facts: four per lock, two per defer, one per value a tracked variable may
// hold, and one per value a load of it may yield.
func (u *universe) index() {
	u.n = 4*len(u.locks) + 2*len(u.defers)

	for pair := range u.holds {
		u.holds[pair] = u.n
		u.n++
	}

	for pair := range u.is {
		u.is[pair] = u.n
		u.n++
	}
}

func (u *universe) held(i int) int     { return 4 * i }
func (u *universe) excl(i int) int     { return 4*i + 1 }
func (u *universe) free(i int) int     { return 4*i + 2 }
func (u *universe) notExcl(i int) int  { return 4*i + 3 }
func (u *universe) pending(j int) int  { return 4*len(u.locks) + 2*j }
func (u *universe) unpended(j int) int { return 4*len(u.locks) + 2*j + 1 }

// indexID numbers the index key k.
func (u *universe) indexID(k key) int {
	id, ok := u.indexIDs[k]
	if !ok {
		id = len(u.indexKeys)
		u.indexIDs[k] = id
		u.indexKeys = append(u.indexKeys, k)
	}

	return id
}
