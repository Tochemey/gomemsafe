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

// Package locks is the Tier 1 check on lock contracts, MS403, which reads gVisor checklocks'
// annotations unchanged.
//
// MS403 is a field or a global accessed without holding the lock its +checklocks annotation
// names, exclusively for a write; a function called without the locks its annotations say
// callers hold, or while holding one they say callers must not; a function returning without a
// lock its annotations say it leaves held, or holding any other; and a lock locked while held,
// or unlocked while not. A lock is held where every path holds it, and may be held where some
// path does; a function that locks on one path and not another is reported at its return, as
// checklocks reports it. The locks a function may hold are named by their root, a parameter,
// a global, a local variable, a result of a call, or a loaded value, and the fields from it, so
// that the locks of two parameters are told apart. What a local pointer variable holds is
// followed through its stores, including those of function literals called directly, so that a
// value loaded before a later store keeps its identity.
//
// A function literal called or deferred directly is analysed where it is called, with the
// caller's lock state, and the state at its returns continues after the call; a literal passed
// on, stored, or started as a goroutine is analysed on its own, with no lock held, since
// nothing is known of when it runs. Deferred calls apply where the function returns, in reverse
// order; a deferred call pending on some paths only is assumed to pair with the acquire on
// those paths, so its releases apply and nothing in it is reported. A loop body is analysed with
// the state it is first entered with, as checklocks does: a lock a later iteration leaves held
// is not reported after the loop. Only functions that use an annotated object are analysed,
// so unannotated code is never reported.
package locks

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/catalog"
	"github.com/tochemey/gomemsafe/internal/directive"
	"github.com/tochemey/gomemsafe/internal/flow"
	"github.com/tochemey/gomemsafe/internal/lazyssa"
	"github.com/tochemey/gomemsafe/internal/prefilter"
	"github.com/tochemey/gomemsafe/internal/report"
	"github.com/tochemey/gomemsafe/internal/summary"
	"github.com/tochemey/gomemsafe/internal/trace"
	"github.com/tochemey/gomemsafe/internal/vetmode"
)

// ID is the check ID of a lock contract violated.
const ID = catalog.UnguardedField

// Diagnostic texts of MS403. The first %s after the ID is what is accessed, called, or
// returned from; the lock follows.
const (
	msgAccess              = "%s: %s %s here without holding %s"
	msgCallWithout         = "%s: %s called here without holding %s, which it requires"
	msgCallReleasing       = "%s: %s called here to release %s, which is not held"
	msgCallHolding         = "%s: %s called here while holding %s, which it must not be"
	msgCallAcquiring       = "%s: %s called here to acquire %s, which is already held"
	msgLocked              = "%s: %s locked here while already held"
	msgUnlocked            = "%s: %s unlocked here while not held"
	msgReturnWithout       = "%s: %s returns here without holding %s, which it must leave held"
	msgReturnHolding       = "%s: %s returns here holding %s"
	msgReturnSomePaths     = "%s: %s returns holding %s on some paths and not on others"
	msgGuardedHere         = "%s is guarded by %s here"
	msgContractHere        = "the contract of %s is declared here"
	helpAccess             = "lock %s before this access, or declare that the function needs it held with // +checklocks:%s"
	helpLockBefore         = "lock %s before this call, or declare that the function needs it held with // +checklocks:%s"
	helpUnlockBefore       = "unlock %s before this call"
	helpLock               = "lock %s only where it is not held, or unlock it first"
	helpUnlock             = "unlock %s only where it is held, in the mode it was locked"
	helpLockBeforeReturn   = "lock %s before returning"
	helpUnlockBeforeReturn = "unlock %s before returning, or declare that the function leaves it held with // +checklocksacquire:%s"
)

// budget is the dataflow budget per function; zero means flow.DefaultBudget. Tests lower it to
// see what a function over budget does: it reports nothing.
var budget int

// Analyzer is the locks check.
var Analyzer = &analysis.Analyzer{
	Name: "locks",
	Doc: "reports fields accessed without holding the lock their checklocks annotation names, " +
		"and functions called or returning in breach of their lock annotations (MS403)",
	Requires: []*analysis.Analyzer{
		directive.Analyzer, prefilter.Analyzer, lazyssa.Analyzer, summary.Analyzer,
	},
	Run: run,
}

// facts is what the check needs from the summary analyzer: the guards of a field or a global,
// the lock contract of a function, and whether an object takes part in lock checking at all.
// The summary's Result provides it; tests stub it.
type facts interface {
	GuardsOf(types.Object) []directive.Guard
	LockContractOf(*types.Func) (*directive.LockContract, bool)
	HasLockFacts(types.Object) bool
}

type checker struct {
	pass *analysis.Pass
	dir  *directive.Result
	sum  facts
	eng  flow.Engine
	// seen holds the diagnostics emitted, so that a literal inlined at two calls reports an
	// access once.
	seen map[diagnosticKey]bool
	// uses caches, by function and object, the positions where the function's source uses the
	// object, for accesses whose instruction SSA gives no position.
	uses map[*ssa.Function]map[types.Object][]token.Pos
}

// diagnosticKey tells diagnostics apart.
type diagnosticKey struct {
	pos token.Pos
	msg string
}

// run returns at once in facts-only runs: unitchecker runs locks on every dependency only
// because it requires the summary analyzer. Only functions that use an annotated object are
// lowered to SSA.
func run(pass *analysis.Pass) (any, error) {
	if vetmode.FactsOnly() {
		trace.Record(pass.Pkg.Path(), pass.Analyzer.Name, trace.KindSkipped, 0)
		return nil, nil
	}

	pre := pass.ResultOf[prefilter.Analyzer].(*prefilter.Result)
	c := &checker{
		pass: pass, dir: pass.ResultOf[directive.Analyzer].(*directive.Result),
		sum: pass.ResultOf[summary.Analyzer].(*summary.Result), eng: flow.Engine{Budget: budget},
		seen: make(map[diagnosticKey]bool), uses: make(map[*ssa.Function]map[types.Object][]token.Pos),
	}

	var funcs []*prefilter.Func

	for _, f := range pre.Funcs {
		if c.annotated(f) {
			funcs = append(funcs, f)
		}
	}

	if len(funcs) == 0 {
		return nil, nil
	}

	ssaRes := pass.ResultOf[lazyssa.Analyzer].(*lazyssa.Result)

	for _, f := range funcs {
		contract, _ := c.sum.LockContractOf(f.Obj)
		if contract != nil && contract.Ignore {
			continue
		}

		if fn := ssaRes.Func(f.Obj); fn != nil {
			c.function(fn, contract)
		}
	}

	for range c.eng.BudgetHits {
		trace.Record(pass.Pkg.Path(), pass.Analyzer.Name, trace.KindBudget, 0)
	}

	return nil, nil
}

// annotated reports whether f takes part in lock checking: it has a contract, or its body
// uses a guarded field or global, a lock an annotation names, or a function with a contract.
func (c *checker) annotated(f *prefilter.Func) bool {
	if c.sum.HasLockFacts(f.Obj) {
		return true
	}

	found := false

	ast.Inspect(f.Decl.Body, func(n ast.Node) bool {
		if found {
			return false
		}

		switch x := n.(type) {
		case *ast.SelectorExpr:
			if sel, ok := c.pass.TypesInfo.Selections[x]; ok {
				found = c.sum.HasLockFacts(sel.Obj())
			}
		case *ast.Ident:
			if obj := c.pass.TypesInfo.Uses[x]; obj != nil && obj.Pkg() != nil && obj.Parent() == obj.Pkg().Scope() {
				found = c.sum.HasLockFacts(obj)
			}
		}

		return !found
	})

	return found
}

// function analyses fn with its contract, then every function literal in it that is not
// analysed in place at a direct call, on its own. Diagnostics come from the problem's
// transfer function while the solution is replayed, since a literal inlined at a call is
// solved inside that transfer; the walk's own visitor has nothing to add.
func (c *checker) function(fn *ssa.Function, contract *directive.LockContract) {
	u := newUniverse(fn, contract, c)
	p := &locking{c: c, u: u, frames: []*frame{u.root}, budget: cmp.Or(c.eng.Budget, flow.DefaultBudget)}

	if sol, outcome := c.eng.Solve(fn, p); outcome == flow.Solved && !p.unknown {
		p.reporting = true

		sol.Walk(func(ssa.Instruction, *flow.Bitset) {})
		p.inconsistentReturns()
	}

	c.literals(fn, u)
}

// literals analyses on its own every literal nested in fn that escapes.
func (c *checker) literals(fn *ssa.Function, u *universe) {
	for _, lit := range fn.AnonFuncs {
		if u.escapes(lit) {
			c.function(lit, nil)
		} else {
			c.literals(lit, u)
		}
	}
}

// forced reports whether the line of pos carries a +checklocksforce annotation.
func (c *checker) forced(pos token.Pos) bool { return c.dir.Forced(c.pass.Fset, pos) }

// accessPos returns where to report an access to obj by instr: its own position, or, when
// SSA gives it none, such as a conversion of a global's address to an interface, the first
// place the function's source uses obj from the line of the last positioned instruction
// before instr in its block. SSA keeps source order within a block.
func (c *checker) accessPos(instr ssa.Instruction, obj types.Object, f *frame) token.Pos {
	if pos := instr.Pos(); pos.IsValid() {
		return pos
	}

	syntax := f.fn.Syntax()
	lower := syntax.Pos()
	instrs := instr.Block().Instrs

	for i := slices.Index(instrs, instr) - 1; i >= 0; i-- {
		if pos := instrs[i].Pos(); pos.IsValid() && pos >= syntax.Pos() && pos <= syntax.End() {
			lower = pos
			break
		}
	}

	// The bound is a line: the instructions of one statement, such as the slice a variadic call
	// makes, are placed anywhere in it.
	line := c.pass.Fset.Position(lower).Line
	uses := c.usesOf(f.fn, obj)
	i := slices.IndexFunc(uses, func(pos token.Pos) bool { return c.pass.Fset.Position(pos).Line >= line })

	// The source names what its instructions access, so there is a use, and one from the line,
	// since the bound precedes the access in source order; the first use stands in otherwise.
	return uses[max(i, 0)]
}

// usesOf returns, in source order, the positions where fn's source uses obj.
func (c *checker) usesOf(fn *ssa.Function, obj types.Object) []token.Pos {
	uses, ok := c.uses[fn]
	if !ok {
		uses = make(map[types.Object][]token.Pos)
		c.uses[fn] = uses

		ast.Inspect(fn.Syntax(), func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if used := c.pass.TypesInfo.Uses[id]; used != nil {
					uses[used] = append(uses[used], id.Pos())
				}
			}

			return true
		})
	}

	return uses[obj]
}

func (c *checker) reportAccess(pos token.Pos, obj types.Object, lock string, write bool) {
	verb := "read"
	if write {
		verb = "written"
	}

	name := obj.Name()
	if g, ok := obj.(*types.Var); ok && !g.IsField() && g.Pkg() != c.pass.Pkg {
		name = g.Pkg().Name() + "." + name
	}

	c.report(analysis.Diagnostic{
		Pos:      pos,
		Category: ID.String(),
		Message:  fmt.Sprintf(msgAccess, ID, name, verb, lock),
		Related: []analysis.RelatedInformation{
			{Pos: obj.Pos(), Message: fmt.Sprintf(msgGuardedHere, obj.Name(), lock)},
		},
		SuggestedFixes: []analysis.SuggestedFix{{Message: fmt.Sprintf(helpAccess, lock, lock)}},
	})
}

func (c *checker) reportCall(pos token.Pos, fn *types.Func, lock, msg, help string) {
	c.report(analysis.Diagnostic{
		Pos:      pos,
		Category: ID.String(),
		Message:  fmt.Sprintf(msg, ID, fn.Name(), lock),
		Related: []analysis.RelatedInformation{
			{Pos: fn.Pos(), Message: fmt.Sprintf(msgContractHere, fn.Name())},
		},
		SuggestedFixes: []analysis.SuggestedFix{{Message: fmt.Sprintf(help, lock, lock)}},
	})
}

func (c *checker) reportLock(pos token.Pos, lock, msg string) {
	help := helpLock
	if msg == msgUnlocked {
		help = helpUnlock
	}

	c.report(analysis.Diagnostic{
		Pos:            pos,
		Category:       ID.String(),
		Message:        fmt.Sprintf(msg, ID, lock),
		SuggestedFixes: []analysis.SuggestedFix{{Message: fmt.Sprintf(help, lock)}},
	})
}

func (c *checker) reportReturn(pos token.Pos, fn *ssa.Function, lock, msg, help string) {
	c.report(analysis.Diagnostic{
		Pos:            pos,
		Category:       ID.String(),
		Message:        fmt.Sprintf(msg, ID, fn.Name(), lock),
		SuggestedFixes: []analysis.SuggestedFix{{Message: fmt.Sprintf(help, lock, lock)}},
	})
}

// report emits d once.
func (c *checker) report(d analysis.Diagnostic) {
	k := diagnosticKey{d.Pos, d.Message}
	if c.seen[k] {
		return
	}

	c.seen[k] = true

	report.Report(c.pass, d)
}
