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
// callees each function declaration calls, including calls inside its function literals. It
// also indexes every call by the position of its opening parenthesis, which is the position
// SSA gives call instructions, so checks can map an SSA call back to its source.
//
// The summary analyzer uses the callees to decide which functions are worth lowering to SSA
// at all: most functions call nothing that invalidates, and they are never analysed further
// (docs/design.md §7.2).
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
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	res := &Result{Calls: make(map[token.Pos]*ast.CallExpr)}

	var current *Func

	seen := make(map[*types.Func]bool)

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
		case *ast.CallExpr:
			res.Calls[n.Lparen] = n
			if current == nil || n.Pos() < current.Decl.Pos() || n.End() > current.Decl.End() {
				return
			}

			if callee := typeutil.StaticCallee(pass.TypesInfo, n); callee != nil && !seen[callee] {
				seen[callee] = true
				current.Callees = append(current.Callees, callee)
			}
		}
	})

	return res, nil
}
