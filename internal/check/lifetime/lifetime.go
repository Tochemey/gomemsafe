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

// Package lifetime checks values used after they stopped being valid. Using one is a logical
// use-after-free: the garbage collector keeps the memory alive, but something else may already
// own or overwrite it.
//
//   - MS301 is a value used after a call took it back, such as a buffer used after it was
//     returned to a sync.Pool.
//   - MS302 is a view used after its owner ended it, such as the slice from bytes.Buffer.Bytes
//     used after buf.Reset (file views.go).
//
// The check reports a use only when the ending call ran on every path to it (a must
// analysis), so findings are almost always real bugs. Most functions are never lowered to SSA:
// the check looks only at functions that call an invalidating function, which the summary
// analyzer marks, or that call both a view and a call that can end one. It lowers those to
// SSA through lazyssa and asks package invalidation for the uses. Which calls invalidate,
// return views, or end them comes from package effect (known library effects), from
// //memsafe:viewof and //memsafe:invalidates directives, and from summary facts (code that
// calls any of those, in any package).
//
// Every diagnostic carries the use position, related positions at the calls involved, and a
// help text as a suggested fix without edits, which the front-end prints as "help:".
package lifetime

import (
	"cmp"
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

// Check IDs.
const (
	// InvalidationID is the check ID of a use after invalidation.
	InvalidationID = catalog.UseAfterInvalidation
	// ViewID is the check ID of a view used after its owner ended it.
	ViewID = catalog.UseOfInvalidatedView
)

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
	Doc: "reports uses of a value after it was returned to a sync.Pool or otherwise invalidated (MS301), " +
		"and of a view after its owner ended it (MS302)",
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
	pre := pass.ResultOf[prefilter.Analyzer].(*prefilter.Result)

	funcs := checkedFuncs(pre, sum)
	if len(funcs) == 0 {
		return nil, nil
	}

	ssaRes := pass.ResultOf[lazyssa.Analyzer].(*lazyssa.Result)

	c := &checker{pass: pass, sum: sum, calls: pre.Calls}

	for _, f := range funcs {
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
	if a := invalidation.AnalyzeWithViews(fn, c.sum.Lookup(), &c.eng); a != nil {
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
	if expr := c.useOperand(u); expr != nil {
		return expr.Pos()
	}

	// A use with no operand found is shown at its instruction, or at the site when SSA gives the
	// instruction no position.
	return cmp.Or(u.Instr.Pos(), u.Site.Call.Pos())
}

// useOperand returns the source expression that carries the used value, or nil when it cannot
// be found. Calls include go and defer statements.
func (c *checker) useOperand(u invalidation.Use) ast.Expr {
	if call, ok := u.Instr.(ssa.CallInstruction); ok {
		if expr := c.callOperand(call, u.Value); expr != nil {
			return expr
		}
	}

	return c.operandAt(u.Instr, u.Value, u.Instr.Pos())
}

// callOperand returns the source expression of v in call: its receiver or one of its
// arguments. The call is found by its opening parenthesis, which is where a go or defer
// statement's call starts too.
func (c *checker) callOperand(call ssa.CallInstruction, v ssa.Value) ast.Expr {
	expr, ok := c.calls[call.Common().Pos()]
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
	if expr := storedOperand(instr, v, path, pos); expr != nil {
		return expr
	}

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
		case *ast.CallExpr:
			// A conversion, such as string(b): SSA places it at the parenthesis.
			if e.Lparen == pos && len(e.Args) == 1 {
				return e.Args[0]
			}
		case *ast.RangeStmt:
			// A range over a map or a channel: SSA places it at the for keyword.
			if e.For == pos {
				return e.X
			}
		case *ast.SendStmt:
			if e.Arrow == pos {
				return e.Value
			}
		case *ast.ReturnStmt:
			// A return: SSA places it at the keyword, with the results in order.
			if ret, ok := instr.(*ssa.Return); ok && e.Return == pos && len(e.Results) == len(ret.Results) {
				for i, result := range ret.Results {
					if result == v {
						return e.Results[i]
					}
				}
			}
		}
	}

	// A value stored as it is, such as an argument packed into a variadic call's slice, is
	// placed at the value itself.
	if id, ok := path[0].(*ast.Ident); ok && id.Pos() == pos {
		return id
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
	if u.View != nil {
		c.reportView(u)
		return
	}

	pos := c.usePos(u)
	arg := c.siteArg(u.Site, u.Arg)
	name, argText := valueName(arg), exprText(arg)
	callee := calleeName(u.Site.Callee)
	description := c.sum.Describe(u.Site.Callee)

	d := analysis.Diagnostic{Pos: pos, Category: InvalidationID.String()}
	if description != "" {
		d.Message = fmt.Sprintf(msgUseAfterEffect, InvalidationID, name, description)
		d.Related = []analysis.RelatedInformation{{
			Pos: u.Site.Call.Pos(), Message: fmt.Sprintf(msgEffectHere, name, description),
		}}
		d.SuggestedFixes = []analysis.SuggestedFix{{
			Message: fmt.Sprintf(helpMoveEffect, c.funText(u.Site), argText, c.funText(u.Site), argText),
		}}
	} else {
		d.Message = fmt.Sprintf(msgUseAfterCall, InvalidationID, name, callee)
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

// storedOperand returns the source of v when instr stores it. SSA places a store inside the
// left-hand side it writes, such as at f in h.f = b, so the stored value is the right-hand side
// of that assignment; a field of a composite literal is placed at its colon; and a select is
// placed at its keyword, with its cases in source order.
func storedOperand(instr ssa.Instruction, v ssa.Value, path []ast.Node, pos token.Pos) ast.Expr {
	switch x := instr.(type) {
	case *ssa.Store:
		if x.Val != v {
			return nil
		}
	case *ssa.MapUpdate:
		if x.Value != v {
			return nil
		}
	case *ssa.Select:
	default:
		return nil
	}

	for _, n := range path {
		switch e := n.(type) {
		case *ast.SelectStmt:
			if sel, ok := instr.(*ssa.Select); ok && e.Select == pos {
				return sentOperand(sel, v, e)
			}
		case *ast.KeyValueExpr:
			if e.Colon == pos {
				return e.Value
			}
		case *ast.AssignStmt:
			for i, lhs := range e.Lhs {
				if len(e.Lhs) == len(e.Rhs) && lhs.Pos() <= pos && pos < lhs.End() {
					return e.Rhs[i]
				}
			}
		}
	}

	return nil
}

// sentOperand returns the value a case of the select statement stmt sends, when it sends v.
// The select's states are its cases in source order, the default left out.
func sentOperand(sel *ssa.Select, v ssa.Value, stmt *ast.SelectStmt) ast.Expr {
	var cases []*ast.CommClause

	for _, s := range stmt.Body.List {
		if clause := s.(*ast.CommClause); clause.Comm != nil {
			cases = append(cases, clause)
		}
	}

	for i, state := range sel.States {
		if send, ok := cases[i].Comm.(*ast.SendStmt); ok && state.Send == v {
			return send.Value
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
