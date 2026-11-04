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

package ownership

import (
	"fmt"
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/catalog"
	"github.com/tochemey/gomemsafe/internal/effect"
	"github.com/tochemey/gomemsafe/internal/flow"
	"github.com/tochemey/gomemsafe/internal/prefilter"
	"github.com/tochemey/gomemsafe/internal/report"
	"github.com/tochemey/gomemsafe/internal/summary"
)

// AppendID is the check ID of a slice used after an append on a shared prefix.
const AppendID = catalog.AppendAliasing

// AppendAliasFlag names the analyzer flag that extends MS503 to every slice.
const AppendAliasFlag = "appendalias"

// Diagnostic texts of MS503. The slice's source text is the first %s after the ID.
const (
	msgOverwritten   = "%s: %s used after an append may have overwritten it"
	msgOverwriteHere = "%s overwritten here: append on its prefix %s"
	helpOverwrite    = "append to a capped prefix, %s, which append copies, or use %s before the append"
	cappedPrefix     = "a full slice expression capped at its length"
	appendAliasUsage = "report append aliasing (MS503) on every slice, not only owned and borrowed ones"
)

// checkEverySlice is set by the -ownership.appendalias flag.
var checkEverySlice bool

func init() {
	Analyzer.Flags.BoolVar(&checkEverySlice, AppendAliasFlag, false, appendAliasUsage)
}

// appends reports the uses of fn's followed slices after an append may have overwritten them,
// in fn and in every function literal nested in it. decl is fn's declaration, and borrowed the
// parameters its contract borrows.
func (c *checker) appends(fn *ssa.Function, decl *ast.FuncDecl, borrowed effect.ArgSet) {
	a := newAliasing(fn, func(v ssa.Value) bool {
		if checkEverySlice || c.sum.Owned(v.Type()) {
			return true
		}

		p, isParam := v.(*ssa.Parameter)

		return isParam && borrowed.Has(slices.Index(fn.Params, p))
	})

	if a != nil {
		if sol, outcome := c.eng.Solve(fn, a); outcome == flow.Solved {
			reported := make(map[int]bool)

			sol.Walk(func(instr ssa.Instruction, state *flow.Bitset) {
				c.reportOverwritten(a, decl, instr, state, reported)
			})
		}
	}

	for _, anon := range fn.AnonFuncs {
		c.appends(anon, decl, 0)
	}
}

// reportOverwritten reports the first use of each class of a that instr makes where an append
// may have overwritten the slice. Nothing has run before the function's first instruction, whose
// state does not yet hold the parameters.
func (c *checker) reportOverwritten(a *aliasing, decl *ast.FuncDecl, instr ssa.Instruction, state *flow.Bitset, reported map[int]bool) {
	if !a.uses(instr) || instr == a.fn.Blocks[0].Instrs[0] {
		return
	}

	var ops [8]*ssa.Value

	for _, op := range instr.Operands(ops[:0]) {
		if op == nil || *op == nil {
			continue
		}

		class, tracked := a.classOf[*op]
		if !tracked || reported[class] || a.intact(instr, class, state) {
			continue
		}

		reported[class] = true

		ow := a.lastOverwrite(class, instr.Pos())
		name := valueName(c.loc.Operand(instr, *op))
		prefix := sliceExprAt(decl, ow.prefix)

		report.Report(c.pass, analysis.Diagnostic{
			Pos:      c.loc.UsePos(instr, *op, ow.call.Pos()),
			Category: AppendID.String(),
			Message:  fmt.Sprintf(msgOverwritten, AppendID, name),
			Related: []analysis.RelatedInformation{{
				Pos: ow.call.Pos(), Message: fmt.Sprintf(msgOverwriteHere, name, valueName(prefix)),
			}},
			SuggestedFixes: []analysis.SuggestedFix{{Message: fmt.Sprintf(helpOverwrite, cappedText(prefix), name)}},
		})
	}
}

// mayOverwrite reports whether f appends to a reslice with a high bound and handles a slice
// MS503 follows: every slice with -ownership.appendalias, otherwise a value of an owned slice
// type or a parameter that borrowed lists. Only such functions are lowered to SSA.
func mayOverwrite(info *types.Info, sum *summary.Result, borrowed effect.ArgSet, f *prefilter.Func) bool {
	appends, reslices := false, false
	followed := checkEverySlice || borrowsSlice(f.Obj, borrowed)

	ast.Inspect(f.Decl.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			if id, ok := ast.Unparen(n.Fun).(*ast.Ident); ok {
				if b, isBuiltin := info.Uses[id].(*types.Builtin); isBuiltin && b.Name() == appendBuiltin {
					appends = true
				}
			}
		case *ast.SliceExpr:
			reslices = reslices || n.High != nil
		case *ast.Ident:
			if v, ok := info.ObjectOf(n).(*types.Var); ok && isSlice(v.Type()) && sum.Owned(v.Type()) {
				followed = true
			}
		}

		return !appends || !reslices || !followed
	})

	return appends && reslices && followed
}

// borrowsSlice reports whether borrowed names a slice parameter of fn, the receiver first.
func borrowsSlice(fn *types.Func, borrowed effect.ArgSet) bool {
	sig := fn.Signature()

	var params []*types.Var

	if sig.Recv() != nil {
		params = append(params, sig.Recv())
	}

	for v := range sig.Params().Variables() {
		params = append(params, v)
	}

	for i, p := range params {
		if borrowed.Has(i) && isSlice(p.Type()) {
			return true
		}
	}

	return false
}

// sliceExprAt returns the slice expression of decl that s was built from, or nil when decl has
// none, as for a reslice the compiler made.
func sliceExprAt(decl *ast.FuncDecl, s *ssa.Slice) *ast.SliceExpr {
	var found *ast.SliceExpr

	ast.Inspect(decl, func(n ast.Node) bool {
		if se, ok := n.(*ast.SliceExpr); ok && se.Lbrack == s.Pos() {
			found = se
		}

		return found == nil
	})

	return found
}

// cappedText returns prefix rewritten as a full slice expression whose capacity ends where its
// length does, such as a[:i:i] for a[:i].
func cappedText(prefix *ast.SliceExpr) string {
	if prefix == nil {
		return cappedPrefix
	}

	low := ""
	if prefix.Low != nil {
		low = types.ExprString(prefix.Low)
	}

	high := types.ExprString(prefix.High)

	return fmt.Sprintf("%s[%s:%s:%s]", types.ExprString(prefix.X), low, high, high)
}
