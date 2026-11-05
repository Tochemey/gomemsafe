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
// callees and interface methods each function declaration calls, including calls inside its
// function literals, and which of them it passes one of its own parameters, whether it calls an interface method or a
// function value, whether it communicates on a channel, and where it names a parameter whose
// memory could leave the function (file escapes.go). It also indexes every call by the
// position of its opening parenthesis, which is the position SSA gives call instructions, so
// checks can map an SSA call back to its source.
//
// The summary analyzer uses the callees to decide which functions are worth lowering to SSA
// at all: most functions call nothing that invalidates, makes a view of a parameter, or ends
// one, and keep no parameter, and they are never analysed further.
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
	Doc:        "records the static callees and interface methods each function declaration calls",
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

// Func is one function declaration and the callees in its body, including the bodies of the
// function literals it contains.
type Func struct {
	Obj  *types.Func
	Decl *ast.FuncDecl
	// Callees lists the static callees, and the interface methods called, whose contracts bind
	// those calls.
	Callees []*types.Func
	// ParamCallees lists the callees that a call passes one of the declaration's parameters, the
	// receiver included: as an argument, or as the receiver of a method declared on the
	// parameter's own type. Only a parameter written as itself counts, not a field of it, and
	// not one that a method promoted from an embedded field or an implicit address takes.
	ParamCallees []*types.Func
	// CallsDynamic reports whether the body calls an interface method or a function value, whose
	// callee is not known statically.
	CallsDynamic bool
	// Communicates reports whether the body sends on or receives from a channel, in a select
	// or not, including a range over a channel.
	Communicates bool
	// ParamStored reports whether the body names a memory-carrying parameter, the receiver
	// included, where its memory could leave the function by itself: in a return, a send, a go
	// statement, a composite literal, a function literal, the value or the map key of an
	// assignment, a variable declaration, a range whose elements carry memory, an address-of,
	// a call of a builtin that may keep it, or the arguments of a function literal called. A
	// parameter only read, compared, or written into stays within the call, as does one the
	// call copies.
	ParamStored bool
	// ArgCallees lists the static callees of the calls whose receiver or arguments name a
	// memory-carrying parameter: what they keep decides whether it escapes.
	ArgCallees []*types.Func
	// ResultCallees lists those of ArgCallees whose call's result is used where it could leave
	// the function: in one of the places ParamStored looks at, or as the receiver or an
	// argument of another call. Whether the result shares an argument's memory decides
	// whether the parameter escapes through it.
	ResultCallees []*types.Func
	// ParamDynamic reports whether the body calls an interface method or a function value with
	// a receiver or arguments naming a memory-carrying parameter, unless a known effect says
	// the method keeps nothing.
	ParamDynamic bool
}

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	res := &Result{Calls: make(map[token.Pos]*ast.CallExpr)}

	var current *Func

	seen := make(map[*types.Func]bool)
	seenWithParam := make(map[*types.Func]bool)
	seenWithArg := make(map[*types.Func]bool)
	seenWithResult := make(map[*types.Func]bool)
	used := make(map[*ast.CallExpr]bool)
	params := make(map[types.Object]bool)

	nodes := []ast.Node{
		(*ast.FuncDecl)(nil), (*ast.CallExpr)(nil), (*ast.SendStmt)(nil), (*ast.UnaryExpr)(nil), (*ast.RangeStmt)(nil),
		(*ast.ReturnStmt)(nil), (*ast.AssignStmt)(nil), (*ast.GoStmt)(nil), (*ast.CompositeLit)(nil), (*ast.FuncLit)(nil),
		(*ast.ValueSpec)(nil),
	}

	insp.Preorder(nodes, func(n ast.Node) {
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
			clear(seenWithArg)
			clear(seenWithResult)
			clear(used)
			clear(params)
			collectParams(pass, n, params)
		case *ast.CallExpr:
			res.Calls[n.Lparen] = n
			if current == nil || n.Pos() < current.Decl.Pos() || n.End() > current.Decl.End() {
				return
			}

			current.ParamStored = current.ParamStored || namesParam(pass, storedParts(pass, n, used), params, used)

			callee := typeutil.StaticCallee(pass.TypesInfo, n)
			if callee == nil {
				current.CallsDynamic = current.CallsDynamic || isDynamic(pass, n)
				current.ParamDynamic = current.ParamDynamic || mayKeepDynamically(pass, n, params, used)

				// A call with no static callee that names a function calls an interface method,
				// which ParamDynamic already answers for keeping what it is passed.
				if method, isMethod := typeutil.Callee(pass.TypesInfo, n).(*types.Func); isMethod {
					current.addCallee(pass, method, n, params, seen, seenWithParam)
				}

				return
			}

			current.addArgCallee(callee, namesParam(pass, callParts(n), params, used), used[n], seenWithArg, seenWithResult)
			current.addCallee(pass, callee, n, params, seen, seenWithParam)
		default:
			if current == nil || n.Pos() < current.Decl.Pos() || n.End() > current.Decl.End() {
				return
			}

			current.Communicates = current.Communicates || communicates(pass, n)
			current.ParamStored = current.ParamStored || namesParam(pass, storedParts(pass, n, used), params, used)
		}
	})

	return res, nil
}

// addCallee records that call calls callee, and passes it a parameter if it does.
func (f *Func) addCallee(pass *analysis.Pass, callee *types.Func, call *ast.CallExpr, params map[types.Object]bool, seen, seenWithParam map[*types.Func]bool) {
	if !seen[callee] {
		seen[callee] = true
		f.Callees = append(f.Callees, callee)
	}

	if !seenWithParam[callee] && passesParam(pass, call, params) {
		seenWithParam[callee] = true
		f.ParamCallees = append(f.ParamCallees, callee)
	}
}

// communicates reports whether n, a send, a unary expression, or a range statement, is a channel
// operation; any other statement is none.
func communicates(pass *analysis.Pass, n ast.Node) bool {
	switch n := n.(type) {
	case *ast.SendStmt:
		return true
	case *ast.UnaryExpr:
		return n.Op == token.ARROW
	case *ast.RangeStmt:
		_, isChan := pass.TypesInfo.TypeOf(n.X).Underlying().(*types.Chan)
		return isChan
	}

	return false
}

// isDynamic reports whether call, which has no static callee, calls an interface method or a
// function value, rather than a builtin, a conversion, or a function literal.
func isDynamic(pass *analysis.Pass, call *ast.CallExpr) bool {
	switch typeutil.Callee(pass.TypesInfo, call).(type) {
	case *types.Func, *types.Var:
		return true
	}

	return false
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
