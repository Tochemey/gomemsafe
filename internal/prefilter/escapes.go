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

package prefilter

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/tochemey/gomemsafe/internal/effect"
	"github.com/tochemey/gomemsafe/internal/escape"
)

// Builtins that keep their arguments in a way of their own.
const (
	builtinAppend = "append"
	builtinCopy   = "copy"
	builtinPanic  = "panic"
)

// keepsNothing lists the builtins that keep nothing of their arguments: they read them, or
// take a type. The others, such as append, copy, panic, and the functions of package unsafe,
// may keep them or return their memory.
var keepsNothing = map[string]bool{
	"len": true, "cap": true, "close": true, "delete": true, "clear": true, "print": true, "println": true,
	"min": true, "max": true, "real": true, "imag": true, "complex": true, "new": true, "make": true,
}

// storedParts returns the parts of n where a value named can leave the function by itself:
// the results of a return, the value sent, the call of a go statement, the elements of a
// composite literal, the body of a function literal, the values and the map keys of an
// assignment, the values of a variable declaration, the expression of a range whose elements
// carry memory, the operand of an address-of, the arguments of a builtin that may keep them,
// and the arguments of a function literal called; of the values, only those that carry
// memory. Other calls are decided by their callees; a unary expression other than an
// address-of, such as a receive, has none.
func storedParts(pass *analysis.Pass, n ast.Node, used map[*ast.CallExpr]bool) []ast.Node {
	switch n := n.(type) {
	case *ast.ReturnStmt:
		return carrying(pass, n.Results)
	case *ast.SendStmt:
		return carrying(pass, []ast.Expr{n.Value})
	case *ast.GoStmt:
		return callParts(n.Call)
	case *ast.CompositeLit:
		return carrying(pass, elements(n.Elts))
	case *ast.FuncLit:
		return []ast.Node{n.Body}
	case *ast.ValueSpec:
		return carrying(pass, n.Values)
	case *ast.AssignStmt:
		return carrying(pass, append(slices.Clone(n.Rhs), mapKeys(n.Lhs)...))
	case *ast.RangeStmt:
		if elementsCarry(pass.TypesInfo.TypeOf(n.X)) {
			return []ast.Node{n.X}
		}
	case *ast.UnaryExpr:
		if n.Op == token.AND {
			return []ast.Node{n.X}
		}
	case *ast.CallExpr:
		return keptArgs(pass, n, used[n])
	}

	return nil
}

// keptArgs returns the arguments of call that the call itself may keep: those of a function
// literal, which is analysed with its caller, and those a builtin may keep. append and copy
// put the elements of a slice into another, which keeps memory only when the elements carry
// it; the result of append shares its first argument, and those of package unsafe's functions
// share theirs, which matters only when the result is used; panic keeps its value.
func keptArgs(pass *analysis.Pass, call *ast.CallExpr, used bool) []ast.Node {
	if _, isLit := ast.Unparen(call.Fun).(*ast.FuncLit); isLit {
		return carrying(pass, call.Args)
	}

	b, isBuiltin := typeutil.Callee(pass.TypesInfo, call).(*types.Builtin)
	if !isBuiltin || keepsNothing[b.Name()] {
		return nil
	}

	var kept []ast.Expr

	switch b.Name() {
	case builtinAppend:
		if used {
			kept = append(kept, call.Args[0])
		}

		if elementsShare(pass.TypesInfo.TypeOf(call.Args[0])) {
			kept = append(kept, call.Args[1:]...)
		}
	case builtinCopy:
		if elementsShare(pass.TypesInfo.TypeOf(call.Args[0])) {
			kept = call.Args[1:]
		}
	case builtinPanic:
		kept = call.Args
	default:
		if used {
			kept = call.Args
		}
	}

	return carrying(pass, kept)
}

// elementsShare reports whether the elements of a slice of type t carry memory.
func elementsShare(t types.Type) bool {
	s, ok := t.Underlying().(*types.Slice)
	return ok && escape.Shares(s.Elem())
}

// carrying returns the expressions of list whose values carry memory: a value that carries
// none, such as a number read from a parameter, takes nothing of it anywhere. The guard of a
// type switch, x.(type), has no type of its own, and binds x's memory to its variable; a call
// with several results carries memory when one of them does.
func carrying(pass *analysis.Pass, list []ast.Expr) []ast.Node {
	var out []ast.Node

	for _, e := range list {
		if t := pass.TypesInfo.TypeOf(e); t == nil || sharesAny(t) {
			out = append(out, e)
		}
	}

	return out
}

// sharesAny reports whether a value of type t, or one of the values of a tuple, carries memory.
func sharesAny(t types.Type) bool {
	tuple, ok := t.(*types.Tuple)
	if !ok {
		return escape.Shares(t)
	}

	for i := range tuple.Len() {
		if escape.Shares(tuple.At(i).Type()) {
			return true
		}
	}

	return false
}

// elements returns the values of a composite literal's elements, without their keys.
func elements(elts []ast.Expr) []ast.Expr {
	out := make([]ast.Expr, len(elts))

	for i, e := range elts {
		out[i] = e
		if kv, ok := e.(*ast.KeyValueExpr); ok {
			out[i] = kv.Value
		}
	}

	return out
}

// addArgCallee records callee, called with a receiver or arguments that name a parameter when
// named, in ArgCallees, and in ResultCallees too when the call's result is used.
func (f *Func) addArgCallee(callee *types.Func, named, used bool, seenWithArg, seenWithResult map[*types.Func]bool) {
	if !named {
		return
	}

	if !seenWithArg[callee] {
		seenWithArg[callee] = true
		f.ArgCallees = append(f.ArgCallees, callee)
	}

	if used && !seenWithResult[callee] {
		seenWithResult[callee] = true
		f.ResultCallees = append(f.ResultCallees, callee)
	}
}

// mayKeepDynamically reports whether call, which has no static callee, calls an interface
// method or a function value that may keep a parameter it is passed: one named in its receiver
// or arguments, unless a known effect says the method keeps nothing. The calls nested in its
// receiver and arguments are marked used.
func mayKeepDynamically(pass *analysis.Pass, call *ast.CallExpr, params map[types.Object]bool, used map[*ast.CallExpr]bool) bool {
	if !isDynamic(pass, call) {
		return false
	}

	named := namesParam(pass, callParts(call), params, used)

	if method, isFunc := typeutil.Callee(pass.TypesInfo, call).(*types.Func); isFunc {
		if _, _, known := effect.Kept(method); known {
			return false
		}
	}

	return named
}

// callParts returns the arguments of call and its receiver, the operand of a method selection.
func callParts(call *ast.CallExpr) []ast.Node {
	parts := exprs(call.Args)
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		parts = append(parts, sel.X)
	}

	return parts
}

// mapKeys returns the keys of the map entries among lhs, the left side of an assignment: a key
// is kept by the map.
func mapKeys(lhs []ast.Expr) []ast.Expr {
	var out []ast.Expr

	for _, e := range lhs {
		if index, ok := ast.Unparen(e).(*ast.IndexExpr); ok {
			out = append(out, index.Index)
		}
	}

	return out
}

// namesParam reports whether one of params that carries memory is named in parts, and marks
// used the calls nested in them. A nested call is not looked into, unless it is a conversion:
// what it does with its arguments, and whether its result shares their memory, is decided
// where the call itself is visited, by its callee.
func namesParam(pass *analysis.Pass, parts []ast.Node, params map[types.Object]bool, used map[*ast.CallExpr]bool) bool {
	found := false

	for _, part := range parts {
		ast.Inspect(part, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.Ident:
				if obj := pass.TypesInfo.Uses[x]; params[obj] && escape.Shares(obj.Type()) {
					found = true
				}
			case *ast.CallExpr:
				if pass.TypesInfo.Types[x.Fun].IsType() {
					return true
				}

				used[x] = true

				return false
			}

			return true
		})
	}

	return found
}

// elementsCarry reports whether what a range over a value of type t yields can carry memory:
// the elements of a slice, an array, or a channel, the keys or values of a map, or whatever a
// function yields. A range over a number or a string yields none.
func elementsCarry(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Slice:
		return escape.Shares(u.Elem())
	case *types.Array:
		return escape.Shares(u.Elem())
	case *types.Pointer:
		return elementsCarry(u.Elem())
	case *types.Chan:
		return escape.Shares(u.Elem())
	case *types.Map:
		return escape.Shares(u.Key()) || escape.Shares(u.Elem())
	case *types.Signature:
		return true
	}

	return false
}

func exprs(list []ast.Expr) []ast.Node {
	out := make([]ast.Node, len(list))
	for i, e := range list {
		out[i] = e
	}

	return out
}
