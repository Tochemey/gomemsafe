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

// Package lifetime is the MS301 check: a value used after a call took it back, such as a
// buffer used after it was returned to a sync.Pool. Using it is a logical use-after-free: the
// garbage collector keeps the memory alive, but the pool may already have handed it to
// someone else.
//
// The check reports a use only when the invalidating call ran on every path to it (a must
// analysis), so findings are almost always real bugs (Tier 1 in docs/design.md §5). It runs
// only on functions the summary analyzer marked as calling an invalidating function, lowers
// them to SSA through lazyssa, and asks package invalidation for the uses. Which calls
// invalidate comes from package effect (known library effects) and from summary facts (code
// that calls those, in any package).
//
// Every diagnostic carries the use position, a related position at the invalidating call,
// and a help text as a suggested fix without edits, which the front-end prints as "help:".
package lifetime

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/catalog"
	"github.com/tochemey/gomemsafe/internal/directive"
	"github.com/tochemey/gomemsafe/internal/flow"
	"github.com/tochemey/gomemsafe/internal/invalidation"
	"github.com/tochemey/gomemsafe/internal/lazyssa"
	"github.com/tochemey/gomemsafe/internal/prefilter"
	"github.com/tochemey/gomemsafe/internal/report"
	"github.com/tochemey/gomemsafe/internal/summary"
	"github.com/tochemey/gomemsafe/internal/trace"
	"github.com/tochemey/gomemsafe/internal/vetmode"
)

// ID is the check ID of use after invalidation.
const ID = catalog.UseAfterInvalidation

// Diagnostic texts. The value's source text is the first %s after the ID.
const (
	msgUseAfterEffect  = "%s: %s used after it was %s"
	msgEffectHere      = "%s %s here"
	helpMoveEffect     = "move %s(%s) after the last use, or use `defer %s(%s)`"
	msgUseAfterCall    = "%s: %s used after the call to %s invalidated it"
	msgInvalidatedHere = "%s invalidated here by %s"
	helpMoveCall       = "move the call to %s after the last use of %s"
	unknownValueText   = "value"
)

// Analyzer is the lifetime check.
var Analyzer = &analysis.Analyzer{
	Name: "lifetime",
	Doc:  "reports uses of a value after it was returned to a sync.Pool or otherwise invalidated (MS301)",
	Requires: []*analysis.Analyzer{
		directive.Analyzer, prefilter.Analyzer, lazyssa.Analyzer, summary.Analyzer,
	},
	Run: run,
}

type checker struct {
	pass  *analysis.Pass
	sum   *summary.Result
	calls map[token.Pos]*ast.CallExpr
	eng   flow.Engine
}

func run(pass *analysis.Pass) (any, error) {
	if vetmode.FactsOnly() {
		trace.Record(pass.Pkg.Path(), pass.Analyzer.Name, trace.KindSkipped, 0)
		return nil, nil
	}

	sum := pass.ResultOf[summary.Analyzer].(*summary.Result)
	if len(sum.Triggered) == 0 {
		return nil, nil
	}

	pre := pass.ResultOf[prefilter.Analyzer].(*prefilter.Result)
	ssaRes := pass.ResultOf[lazyssa.Analyzer].(*lazyssa.Result)

	c := &checker{pass: pass, sum: sum, calls: pre.Calls}
	for _, f := range sum.Triggered {
		fn := ssaRes.Func(f.Obj)
		if fn == nil {
			continue
		}

		c.function(fn)
	}

	for range c.eng.BudgetHits {
		trace.Record(pass.Pkg.Path(), pass.Analyzer.Name, trace.KindBudget, 0)
	}

	return nil, nil
}

// function checks fn and every function literal nested in it.
func (c *checker) function(fn *ssa.Function) {
	if a := invalidation.Analyze(fn, c.sum.Lookup(), &c.eng); a != nil {
		for _, u := range a.Uses() {
			c.report(u)
		}
	}

	for _, anon := range fn.AnonFuncs {
		c.function(anon)
	}
}

// usePos returns the position of the used value in the source. SSA places many uses at an
// operator: a call at its opening parenthesis, a field access at the field name, a comparison
// at its operator. Those are mapped back to the operand that carries the value.
func (c *checker) usePos(u invalidation.Use) token.Pos {
	if call, ok := u.Instr.(*ssa.Call); ok {
		if expr := c.callOperand(call, u.Value); expr != nil {
			return expr.Pos()
		}
	}

	pos := u.Instr.Pos()
	if !pos.IsValid() {
		return u.Site.Call.Pos()
	}

	if expr := c.operandAt(u.Instr, u.Value, pos); expr != nil {
		return expr.Pos()
	}

	return pos
}

// callOperand returns the source expression of v in call: its receiver or one of its
// arguments.
func (c *checker) callOperand(call *ssa.Call, v ssa.Value) ast.Expr {
	expr, ok := c.calls[call.Pos()]
	if !ok {
		return nil
	}

	recv, hasRecv := c.receiver(expr)

	common := call.Common()
	if common.IsInvoke() {
		if hasRecv && common.Value == v {
			return recv
		}

		return argExpr(expr, common.Args, v, 0)
	}

	if hasRecv {
		if len(common.Args) > 0 && common.Args[0] == v {
			return recv
		}

		return argExpr(expr, common.Args, v, 1)
	}

	return argExpr(expr, common.Args, v, 0)
}

// operandAt returns the operand that carries v in the expression whose operator is at pos,
// the position SSA gives instr.
func (c *checker) operandAt(instr ssa.Instruction, v ssa.Value, pos token.Pos) ast.Expr {
	file := c.fileOf(pos)
	if file == nil {
		return nil
	}

	path, _ := astutil.PathEnclosingInterval(file, pos, pos)
	for _, n := range path {
		switch e := n.(type) {
		case *ast.SelectorExpr:
			if e.Sel.Pos() == pos {
				return e.X
			}
		case *ast.BinaryExpr:
			if e.OpPos == pos {
				if op, ok := instr.(*ssa.BinOp); ok && op.X != v {
					return e.Y
				}

				return e.X
			}
		case *ast.IndexExpr:
			if e.Lbrack == pos {
				if isMapKey(instr, v) {
					return e.Index
				}

				return e.X
			}
		case *ast.SliceExpr:
			if e.Lbrack == pos {
				return e.X
			}
		case *ast.StarExpr:
			if e.Star == pos {
				return e.X
			}
		case *ast.UnaryExpr:
			if e.OpPos == pos {
				return e.X
			}
		}
	}

	return nil
}

// fileOf returns the file of the current package that contains pos.
func (c *checker) fileOf(pos token.Pos) *ast.File {
	for _, f := range c.pass.Files {
		if f.FileStart <= pos && pos <= f.FileEnd {
			return f
		}
	}

	return nil
}

// receiver returns the explicit receiver of a method call written as x.M(...). A method
// expression, (*T).M(x, ...), has no explicit receiver: x is its first argument.
func (c *checker) receiver(call *ast.CallExpr) (ast.Expr, bool) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}

	selection, ok := c.pass.TypesInfo.Selections[sel]
	if !ok || selection.Kind() != types.MethodVal {
		return nil, false
	}

	return sel.X, true
}

func (c *checker) report(u invalidation.Use) {
	pos := c.usePos(u)
	arg := c.siteArg(u.Site, u.Arg)
	name, argText := valueName(arg), exprText(arg)
	callee := calleeName(u.Site.Callee)
	description := c.sum.Describe(u.Site.Callee)

	d := analysis.Diagnostic{Pos: pos, Category: ID.String()}
	if description != "" {
		d.Message = fmt.Sprintf(msgUseAfterEffect, ID, name, description)
		d.Related = []analysis.RelatedInformation{{
			Pos: u.Site.Call.Pos(), Message: fmt.Sprintf(msgEffectHere, name, description),
		}}
		d.SuggestedFixes = []analysis.SuggestedFix{{
			Message: fmt.Sprintf(helpMoveEffect, c.funText(u.Site), argText, c.funText(u.Site), argText),
		}}
	} else {
		d.Message = fmt.Sprintf(msgUseAfterCall, ID, name, callee)
		d.Related = []analysis.RelatedInformation{{
			Pos: u.Site.Call.Pos(), Message: fmt.Sprintf(msgInvalidatedHere, name, callee),
		}}
		d.SuggestedFixes = []analysis.SuggestedFix{{
			Message: fmt.Sprintf(helpMoveCall, callee, name),
		}}
	}

	report.Report(c.pass, d)
}

// siteArg returns the source expression of the invalidated argument at site, or nil when it
// cannot be found.
func (c *checker) siteArg(site invalidation.Site, arg int) ast.Expr {
	call, ok := c.calls[site.Call.Pos()]
	if !ok {
		return nil
	}

	if recv, ok := c.receiver(call); ok {
		if arg == 0 {
			return recv
		}

		arg--
	}

	if arg < len(call.Args) {
		return call.Args[arg]
	}

	return nil
}

// funText returns the source text of the called function at site, e.g. "pool.Put".
func (c *checker) funText(site invalidation.Site) string {
	if call, ok := c.calls[site.Call.Pos()]; ok {
		return types.ExprString(call.Fun)
	}

	return calleeName(site.Callee)
}

// calleeName returns a name qualified by package name, not import path, as Go code spells
// it: "lib.Release", "(*lib.Buf).Free", or "(*pool.Pool[T]).Put".
func calleeName(fn *types.Func) string {
	fn = fn.Origin()

	if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
		recv := types.TypeString(sig.Recv().Type(), byPackageName)
		if _, isPointer := sig.Recv().Type().(*types.Pointer); isPointer {
			recv = "(" + recv + ")"
		}

		return recv + "." + fn.Name()
	}

	if fn.Pkg() != nil {
		return fn.Pkg().Name() + "." + fn.Name()
	}

	return fn.Name()
}

// byPackageName qualifies type names by package name.
func byPackageName(pkg *types.Package) string { return pkg.Name() }

// argExpr maps the SSA argument equal to v to its source argument; skip is the number of
// leading SSA arguments with no source counterpart (the receiver).
func argExpr(call *ast.CallExpr, args []ssa.Value, v ssa.Value, skip int) ast.Expr {
	for i, a := range args {
		if a == v && i >= skip && i-skip < len(call.Args) {
			return call.Args[i-skip]
		}
	}

	return nil
}

// isMapKey reports whether v is the key, rather than the map, of a map lookup. Slice and array
// indexes are integers, which are never tracked.
func isMapKey(instr ssa.Instruction, v ssa.Value) bool {
	lookup, ok := instr.(*ssa.Lookup)
	return ok && lookup.Index == v && lookup.X != v
}

// valueName names the value an argument expression passes: buf for buf, buf[:0], or (buf).
func valueName(arg ast.Expr) string {
	if arg == nil {
		return unknownValueText
	}

	for {
		switch e := arg.(type) {
		case *ast.ParenExpr:
			arg = e.X
		case *ast.SliceExpr:
			arg = e.X
		default:
			return types.ExprString(arg)
		}
	}
}

// exprText returns the source text of an argument expression.
func exprText(arg ast.Expr) string {
	if arg == nil {
		return unknownValueText
	}

	return types.ExprString(arg)
}
