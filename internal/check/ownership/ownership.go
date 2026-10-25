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

// Package ownership is the family of Tier 2 checks on ownership, MS501 to MS503, which fire
// only on code annotated with //memsafe:owned or //memsafe:consumes. MS501 is implemented.
//
// MS501 is a value used after its ownership moved: a value of an owned type, or a pointer to
// one, sent on a channel or stored into another owner, or any value passed to a consuming
// parameter, then used again. Like Rust's borrow checker, and unlike the Tier 1 checks, it
// reports a use when the value was moved on some path to it, not only on every path, unless the
// variable was assigned again since: within the annotated code, it misses no such use. What
// moves a value is in file events.go, and the dataflow that follows it in moves.go. A select's
// send case moves its value only when that case runs, and a deferred call
// takes its argument only when the function returns.
//
// It follows a value through the function that holds it: its conversions to other types and
// interfaces, phis, and the local variable it was loaded from. It does not follow a value
// through memory, such as a copy kept in a field or a slice before the move, nor into function
// literals, nor through calls of interface methods or function values, whose consuming
// parameters are unknown. Owned types and consuming parameters reach other packages through the
// summary analyzer's facts.
package ownership

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/catalog"
	"github.com/tochemey/gomemsafe/internal/directive"
	"github.com/tochemey/gomemsafe/internal/flow"
	"github.com/tochemey/gomemsafe/internal/lazyssa"
	"github.com/tochemey/gomemsafe/internal/locate"
	"github.com/tochemey/gomemsafe/internal/prefilter"
	"github.com/tochemey/gomemsafe/internal/report"
	"github.com/tochemey/gomemsafe/internal/summary"
	"github.com/tochemey/gomemsafe/internal/trace"
	"github.com/tochemey/gomemsafe/internal/vetmode"
)

// MoveID is the check ID of a use after move.
const MoveID = catalog.UseAfterMove

// Diagnostic texts of MS501. The value's source text is the first %s after the ID.
const (
	msgUsedAfterMove = "%s: %s used after it was moved"
	msgSent          = "%s moved here: sent on a channel"
	msgConsumed      = "%s moved here: passed to the consuming parameter %s of %s"
	msgStored        = "%s moved here: stored into another owner"
	helpMove         = "use %s before it moves, or move a copy and keep the original"
	unknownValueText = "value"
)

// Analyzer is the ownership check.
var Analyzer = &analysis.Analyzer{
	Name: "ownership",
	Doc: "reports uses of a value after its ownership moved (MS501): an //memsafe:owned value sent " +
		"on a channel or stored into another owner, or a value passed to a //memsafe:consumes parameter",
	Requires: []*analysis.Analyzer{
		directive.Analyzer, prefilter.Analyzer, lazyssa.Analyzer, summary.Analyzer,
	},
	Run: run,
}

type checker struct {
	pass  *analysis.Pass
	sum   *summary.Result
	loc   *locate.Locator
	calls map[token.Pos]*ast.CallExpr
	eng   flow.Engine
}

// run returns at once in facts-only runs: unitchecker runs ownership on every dependency only
// because it requires the summary analyzer. It returns at once too where nothing is annotated,
// which is most code.
func run(pass *analysis.Pass) (any, error) {
	if vetmode.FactsOnly() {
		trace.Record(pass.Pkg.Path(), pass.Analyzer.Name, trace.KindSkipped, 0)
		return nil, nil
	}

	sum := pass.ResultOf[summary.Analyzer].(*summary.Result)
	if !sum.Annotated() {
		return nil, nil
	}

	pre := pass.ResultOf[prefilter.Analyzer].(*prefilter.Result)

	var funcs []*prefilter.Func

	for _, f := range pre.Funcs {
		if mayMove(pass.TypesInfo, sum, f) {
			funcs = append(funcs, f)
		}
	}

	if len(funcs) == 0 {
		return nil, nil
	}

	ssaRes := pass.ResultOf[lazyssa.Analyzer].(*lazyssa.Result)
	c := &checker{pass: pass, sum: sum, loc: locate.New(pass, pre.Calls), calls: pre.Calls}

	for _, f := range funcs {
		if fn := ssaRes.Func(f.Obj); fn != nil {
			c.function(fn)
		}
	}

	for range c.eng.BudgetHits {
		trace.Record(pass.Pkg.Path(), pass.Analyzer.Name, trace.KindBudget, 0)
	}

	return nil, nil
}

// function reports the uses after move in fn and in every function literal nested in it.
func (c *checker) function(fn *ssa.Function) {
	m := newMoves(fn, c.sum)
	if len(m.moves) > 0 {
		if sol, outcome := c.eng.Solve(fn, m); outcome == flow.Solved {
			reported := make(map[int]bool)

			sol.Walk(func(instr ssa.Instruction, state *flow.Bitset) {
				c.reportUses(m, instr, state, reported)
			})
		}
	}

	for _, anon := range fn.AnonFuncs {
		c.function(anon)
	}
}

// reportUses reports the first use of each class of m that instr uses where the class may have
// been moved. Nothing has run before the function's first instruction, which no edge leads
// back to, so nothing it uses can have moved; the state before it does not yet hold the
// parameters, which that instruction makes owned.
func (c *checker) reportUses(m *moves, instr ssa.Instruction, state *flow.Bitset, reported map[int]bool) {
	if !isUse(instr) || instr == m.fn.Blocks[0].Instrs[0] {
		return
	}

	var ops [8]*ssa.Value

	for _, op := range instr.Operands(ops[:0]) {
		if op == nil || *op == nil {
			continue
		}

		for _, class := range m.usedClasses(instr, *op) {
			if reported[class] || state.Has(class) {
				continue
			}

			reported[class] = true

			c.report(instr, *op, m.lastMove(class, instr.Pos()))
		}
	}
}

func (c *checker) report(instr ssa.Instruction, v ssa.Value, mv move) {
	name := valueName(c.loc.Operand(instr, v))

	var moved string

	switch mv.kind {
	case sentOnChannel:
		moved = fmt.Sprintf(msgSent, name)
	case passedToConsumer:
		moved = fmt.Sprintf(msgConsumed, name, mv.param.Name(), c.calleeText(mv.instr.(ssa.CallInstruction)))
	case storedIntoOwner:
		moved = fmt.Sprintf(msgStored, name)
	}

	report.Report(c.pass, analysis.Diagnostic{
		Pos:            c.loc.UsePos(instr, v, mv.instr.Pos()),
		Category:       MoveID.String(),
		Message:        fmt.Sprintf(msgUsedAfterMove, MoveID, name),
		Related:        []analysis.RelatedInformation{{Pos: mv.instr.Pos(), Message: moved}},
		SuggestedFixes: []analysis.SuggestedFix{{Message: fmt.Sprintf(helpMove, name)}},
	})
}

// calleeText returns the source text of the function call calls, such as pool.Consume, or the
// callee's name when the call is not in the source.
func (c *checker) calleeText(call ssa.CallInstruction) string {
	if expr, ok := c.calls[call.Common().Pos()]; ok {
		return types.ExprString(expr.Fun)
	}

	return call.Common().StaticCallee().Name()
}

// mayMove reports whether f can move a value: it calls a function with a consuming parameter,
// or it sends on a channel, stores into a field or an element, or builds a composite literal,
// and names a value of an owned type. Only such functions are lowered to SSA.
func mayMove(info *types.Info, sum *summary.Result, f *prefilter.Func) bool {
	for _, callee := range f.Callees {
		if sum.Consumes(callee) != 0 {
			return true
		}
	}

	moves, owned := false, false

	ast.Inspect(f.Decl.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SendStmt, *ast.CompositeLit:
			moves = true
		case *ast.AssignStmt:
			for _, lhs := range n.Lhs {
				switch ast.Unparen(lhs).(type) {
				case *ast.SelectorExpr, *ast.IndexExpr:
					moves = true
				}
			}
		case *ast.Ident:
			if v, ok := info.ObjectOf(n).(*types.Var); ok && sum.Owned(v.Type()) {
				owned = true
			}
		}

		return !moves || !owned
	})

	return moves && owned
}

// isUse reports whether instr uses its operands' values: a conversion to another type, an
// interface or a type assertion, a phi, or a debug reference makes another value of the same
// one, and a comparison with nil reads no owned memory.
func isUse(instr ssa.Instruction) bool {
	switch x := instr.(type) {
	case *ssa.MakeInterface, *ssa.ChangeType, *ssa.ChangeInterface, *ssa.TypeAssert, *ssa.Phi, *ssa.DebugRef:
		return false
	case *ssa.BinOp:
		return (x.Op != token.EQL && x.Op != token.NEQ) || !isNil(x.X) && !isNil(x.Y)
	}

	return true
}

// valueName names the value an operand expression carries, or the value in general when the
// expression is not found.
func valueName(expr ast.Expr) string {
	if expr == nil {
		return unknownValueText
	}

	return types.ExprString(ast.Unparen(expr))
}
