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

// Package locate maps SSA instructions back to the source expressions they come from, so that
// a check's diagnostic points at the value it names rather than at the operator SSA places the
// instruction on. Calls are found by the position of their opening parenthesis, which prefilter
// indexes.
package locate

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/ssa"
)

// Locator finds source expressions in the files of one package.
type Locator struct {
	pass  *analysis.Pass
	calls map[token.Pos]*ast.CallExpr
}

// New returns a Locator for the package of pass, whose calls are indexed by the position of
// their opening parenthesis.
func New(pass *analysis.Pass, calls map[token.Pos]*ast.CallExpr) *Locator {
	return &Locator{pass: pass, calls: calls}
}

// UsePos returns the position of v, used by instr, in the source: the operand that carries it,
// or else instr's position, or else fallback when SSA gives instr none. SSA places many uses at
// an operator: a call at its opening parenthesis, a field access at the field name, a comparison
// at its operator. Those are mapped back to the operand that carries the value.
func (l *Locator) UsePos(instr ssa.Instruction, v ssa.Value, fallback token.Pos) token.Pos {
	if expr := l.Operand(instr, v); expr != nil {
		return expr.Pos()
	}

	return cmp.Or(instr.Pos(), fallback)
}

// Operand returns the source expression that carries v where instr uses it, or nil when it
// cannot be found. Calls include go and defer statements.
func (l *Locator) Operand(instr ssa.Instruction, v ssa.Value) ast.Expr {
	if call, ok := instr.(ssa.CallInstruction); ok {
		if expr := l.callOperand(call, v); expr != nil {
			return expr
		}
	}

	return l.operandAt(instr, v, instr.Pos())
}

// Receiver returns the explicit receiver of a method call written as x.M(...). A method
// expression, (*T).M(x, ...), has no explicit receiver: x is its first argument.
func (l *Locator) Receiver(call *ast.CallExpr) (ast.Expr, bool) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}

	selection, ok := l.pass.TypesInfo.Selections[sel]
	if !ok || selection.Kind() != types.MethodVal {
		return nil, false
	}

	return sel.X, true
}

// callOperand returns the source expression of v in call: its receiver or one of its
// arguments. The call is found by its opening parenthesis, which is where a go or defer
// statement's call starts too.
func (l *Locator) callOperand(call ssa.CallInstruction, v ssa.Value) ast.Expr {
	expr, ok := l.calls[call.Common().Pos()]
	if !ok {
		return nil
	}

	recv, hasRecv := l.Receiver(expr)

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
func (l *Locator) operandAt(instr ssa.Instruction, v ssa.Value, pos token.Pos) ast.Expr {
	file := l.fileOf(pos)
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
func (l *Locator) fileOf(pos token.Pos) *ast.File {
	for _, f := range l.pass.Files {
		if f.FileStart <= pos && pos <= f.FileEnd {
			return f
		}
	}

	return nil
}

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
		case *ast.ReturnStmt:
			// In a function with deferred calls, a return stores its result into the result's
			// slot at the keyword. With several results, which store is which is not known.
			if e.Return == pos && len(e.Results) == 1 {
				return e.Results[0]
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

// isMapKey reports whether v is the key, rather than the map, of a map lookup or update. Slice
// and array indexes are integers, which are never tracked.
func isMapKey(instr ssa.Instruction, v ssa.Value) bool {
	switch x := instr.(type) {
	case *ssa.Lookup:
		return x.Index == v && x.X != v
	case *ssa.MapUpdate:
		return x.Key == v && x.Map != v
	}

	return false
}
