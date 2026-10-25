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
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/catalog"
	"github.com/tochemey/gomemsafe/internal/directive"
	"github.com/tochemey/gomemsafe/internal/flow"
	"github.com/tochemey/gomemsafe/internal/invalidation"
	"github.com/tochemey/gomemsafe/internal/lazyssa"
	"github.com/tochemey/gomemsafe/internal/locate"
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
	loc   *locate.Locator
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

	c := &checker{pass: pass, sum: sum, calls: pre.Calls, loc: locate.New(pass, pre.Calls)}

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

// usePos returns where u's value is written in the source, or the site when SSA gives the use
// no position.
func (c *checker) usePos(u invalidation.Use) token.Pos {
	return c.loc.UsePos(u.Instr, u.Value, u.Site.Call.Pos())
}

// useOperand returns the source expression of u's value, or nil when it cannot be found.
func (c *checker) useOperand(u invalidation.Use) ast.Expr { return c.loc.Operand(u.Instr, u.Value) }

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

	if recv, ok := c.loc.Receiver(call); ok {
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
