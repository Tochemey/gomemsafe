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

// Package prefilter makes one cheap pass over a package's syntax and records which static
// callees each function declaration calls, including calls inside its function literals, and
// which of them it passes one of its own parameters. It also indexes every call by the position
// of its opening parenthesis, which is the position SSA gives call instructions, so checks can
// map an SSA call back to its source.
//
// The summary analyzer uses the callees to decide which functions are worth lowering to SSA
// at all: most functions call nothing that invalidates, makes a view of a parameter, or ends
// one, and they are never analysed further.
package prefilter

import (
	"go/ast"
	"go/token"
	"go/types"
	"reflect"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
)

// Analyzer computes a Result for each package.
var Analyzer = &analysis.Analyzer{
	Name:       "prefilter",
	Doc:        "records the static callees of every function declaration",
	Requires:   []*analysis.Analyzer{inspect.Analyzer},
	Run:        run,
	ResultType: reflect.TypeFor[*Result](),
}

// Result is the call index of one package.
type Result struct {
	// Funcs lists every function declaration with a body, in source order.
	Funcs []*Func
	// Calls maps the position of each call's opening parenthesis to the call. SSA call
	// instructions report that same position.
	Calls map[token.Pos]*ast.CallExpr
}

// Func is one function declaration and the static callees in its body, including the bodies
// of the function literals it contains.
type Func struct {
	Obj     *types.Func
	Decl    *ast.FuncDecl
	Callees []*types.Func
	// ParamCallees lists the callees that a call passes one of the declaration's parameters, the
	// receiver included: as an argument, or as the receiver of a method declared on the
	// parameter's own type. Only a parameter written as itself counts, not a field of it, and
	// not one that a method promoted from an embedded field or an implicit address takes.
	ParamCallees []*types.Func
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	res := &Result{Calls: make(map[token.Pos]*ast.CallExpr)}

	var current *Func

	seen := make(map[*types.Func]bool)
	seenWithParam := make(map[*types.Func]bool)
	params := make(map[types.Object]bool)

	insp.Preorder([]ast.Node{(*ast.FuncDecl)(nil), (*ast.CallExpr)(nil)}, func(n ast.Node) {
		switch n := n.(type) {
		case *ast.FuncDecl:
			current = nil

			obj, ok := pass.TypesInfo.Defs[n.Name].(*types.Func)
			if !ok || n.Body == nil {
				return
			}

			current = &Func{Obj: obj, Decl: n}
			res.Funcs = append(res.Funcs, current)

			clear(seen)
			clear(seenWithParam)
			clear(params)
			collectParams(pass, n, params)
		case *ast.CallExpr:
			res.Calls[n.Lparen] = n
			if current == nil || n.Pos() < current.Decl.Pos() || n.End() > current.Decl.End() {
				return
			}

			callee := typeutil.StaticCallee(pass.TypesInfo, n)
			if callee == nil {
				return
			}

			if !seen[callee] {
				seen[callee] = true
				current.Callees = append(current.Callees, callee)
			}

			if !seenWithParam[callee] && passesParam(pass, n, params) {
				seenWithParam[callee] = true
				current.ParamCallees = append(current.ParamCallees, callee)
			}
		}
	})

	return res, nil
}

// collectParams adds the receiver and parameters of decl to params.
func collectParams(pass *analysis.Pass, decl *ast.FuncDecl, params map[types.Object]bool) {
	var fields []*ast.Field
	if decl.Recv != nil {
		fields = append(fields, decl.Recv.List...)
	}

	for _, field := range append(fields, decl.Type.Params.List...) {
		for _, id := range field.Names {
			if obj := pass.TypesInfo.Defs[id]; obj != nil {
				params[obj] = true
			}
		}
	}
}

// passesParam reports whether call passes one of params as an argument, or as the receiver of
// a method declared on the parameter's own type.
func passesParam(pass *analysis.Pass, call *ast.CallExpr, params map[types.Object]bool) bool {
	isParam := func(e ast.Expr) bool {
		id, ok := ast.Unparen(e).(*ast.Ident)
		return ok && params[pass.TypesInfo.Uses[id]]
	}

	for _, arg := range call.Args {
		if isParam(arg) {
			return true
		}
	}

	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok || !isParam(sel.X) {
		return false
	}

	selection, ok := pass.TypesInfo.Selections[sel]
	if !ok || selection.Kind() != types.MethodVal || len(selection.Index()) != 1 {
		return false
	}

	recv := selection.Obj().(*types.Func).Signature().Recv()

	return types.Identical(recv.Type(), pass.TypesInfo.Uses[ast.Unparen(sel.X).(*ast.Ident)].Type())
}
