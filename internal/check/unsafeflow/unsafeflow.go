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

// Package unsafeflow is the family of checks on unsafe.Pointer, MS101 to MS104. MS101 is
// implemented: a uintptr turned back into a pointer in a way plain vet cannot see.
//
// vet's unsafeptr pass already reports every conversion from a uintptr to unsafe.Pointer
// outside the forms the unsafe package allows, so MS101 does not repeat it. It reports the two
// routes that involve no such conversion:
//
//   - a uintptr's memory read or written as a pointer, such as
//     *(*unsafe.Pointer)(unsafe.Pointer(&u)), with any pointer, map, channel, or function type
//     in place of unsafe.Pointer;
//   - unsafe.Add on a nil pointer with an offset that is not the constant zero, such as
//     unsafe.Add(nil, u).
//
// Both create a pointer from an integer, which the unsafe package's rules forbid: if the
// address is in memory the garbage collector manages, nothing kept that memory alive or
// updated the address when the memory moved. An address outside that memory, such as machine
// code or memory from C or mmap, is reported too, because nothing in the code tells the two
// apart; such a finding is suppressed with //memsafe:allow MS101 and the reason.
//
// In generic code, a type parameter counts as a uintptr, or as a pointer, only when every type
// its constraint allows is one. Only these direct expressions are recognised: an address passed
// through a variable or a call first is not followed, nor is a uintptr that is part of a larger
// value, such as an array of uintptr or a struct whose first field is one.
package unsafeflow

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/tochemey/gomemsafe/internal/catalog"
	"github.com/tochemey/gomemsafe/internal/directive"
	"github.com/tochemey/gomemsafe/internal/report"
)

// ID is the check ID of a uintptr turned back into a pointer.
const ID = catalog.UnsafePointerRoundTrip

// Diagnostic texts. The uintptr's source text is the first %s after the ID.
const (
	msgReinterpreted  = "%s: %s is a uintptr reinterpreted as a pointer"
	helpReinterpreted = "keep the address in an unsafe.Pointer, which the garbage collector tracks, not in a uintptr"
	msgAddToNil       = "%s: unsafe.Add on a nil pointer turns an integer into a pointer"
	helpAddToNil      = "add the offset to the pointer the address came from: unsafe.Add(p, off)"
)

// addBuiltin is the name of unsafe.Add.
const addBuiltin = "Add"

// Analyzer is the unsafeflow check.
var Analyzer = &analysis.Analyzer{
	Name:     "unsafeflow",
	Doc:      "reports uintptr values turned back into pointers in ways go vet's unsafeptr cannot see (MS101)",
	Requires: []*analysis.Analyzer{directive.Analyzer, inspect.Analyzer},
	Run:      run,
}

// run needs no facts-only short-circuit: unsafeflow requires no fact producer, so unitchecker
// does not run it on dependencies at all.
func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)

		if addr, ok := matchReinterpretation(pass.TypesInfo, call); ok {
			value, text := locateUintptr(addr)
			reportAt(pass, value.Pos(), fmt.Sprintf(msgReinterpreted, ID, text), helpReinterpreted)

			return
		}

		if isAddToNil(pass.TypesInfo, call) {
			reportAt(pass, call.Pos(), fmt.Sprintf(msgAddToNil, ID), helpAddToNil)
		}
	})

	return nil, nil
}

func reportAt(pass *analysis.Pass, pos token.Pos, message, help string) {
	report.Report(pass, analysis.Diagnostic{
		Pos:            pos,
		Category:       ID.String(),
		Message:        message,
		SuggestedFixes: []analysis.SuggestedFix{{Message: help}},
	})
}

// matchReinterpretation matches (*P)(unsafe.Pointer(a)), where a points to a uintptr and P is
// a pointer-shaped type, and returns a.
func matchReinterpretation(info *types.Info, call *ast.CallExpr) (ast.Expr, bool) {
	target, ok := convertedType(info, call)
	if !ok {
		return nil, false
	}

	ptr, ok := target.Underlying().(*types.Pointer)
	if !ok || !everyTerm(ptr.Elem(), isPointerShaped) {
		return nil, false
	}

	inner, ok := ast.Unparen(call.Args[0]).(*ast.CallExpr)
	if !ok {
		return nil, false
	}

	if t, ok := convertedType(info, inner); !ok || !isUnsafePointer(t) {
		return nil, false
	}

	addr := ast.Unparen(inner.Args[0])

	pointee, ok := info.TypeOf(addr).Underlying().(*types.Pointer)
	if !ok || !everyTerm(pointee.Elem(), isUintptr) {
		return nil, false
	}

	return addr, true
}

// isAddToNil matches unsafe.Add(nil, n) where n is not the constant zero.
func isAddToNil(info *types.Info, call *ast.CallExpr) bool {
	if len(call.Args) != 2 || !isUnsafeAdd(info, call.Fun) || !isNil(info, call.Args[0]) {
		return false
	}

	offset := info.Types[call.Args[1]].Value

	return offset == nil || constant.Sign(offset) != 0
}

// convertedType returns the type a conversion converts to, if call is a conversion.
func convertedType(info *types.Info, call *ast.CallExpr) (types.Type, bool) {
	tv, ok := info.Types[ast.Unparen(call.Fun)]
	if !ok || !tv.IsType() || len(call.Args) != 1 {
		return nil, false
	}

	return tv.Type, true
}

func isUnsafeAdd(info *types.Info, fun ast.Expr) bool {
	var id *ast.Ident

	switch f := ast.Unparen(fun).(type) {
	case *ast.SelectorExpr:
		id = f.Sel
	case *ast.Ident:
		id = f
	default:
		return false
	}

	b, ok := info.Uses[id].(*types.Builtin)

	return ok && b.Name() == addBuiltin && b.Pkg() == types.Unsafe
}

// isNil reports whether e is nil, possibly converted to other pointer types first.
func isNil(info *types.Info, e ast.Expr) bool {
	e = ast.Unparen(e)
	if info.Types[e].IsNil() {
		return true
	}

	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}

	_, isConversion := convertedType(info, call)

	return isConversion && isNil(info, call.Args[0])
}

// isPointerShaped reports whether a value of type t is a single machine word that the garbage
// collector follows as a pointer.
func isPointerShaped(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Pointer, *types.Map, *types.Chan, *types.Signature:
		return true
	case *types.Basic:
		return u.Kind() == types.UnsafePointer
	}

	return false
}

func isUnsafePointer(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Kind() == types.UnsafePointer
}

func isUintptr(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Kind() == types.Uintptr
}

// locateUintptr returns the expression to point at for the uintptr that addr points to, and
// its name: u for &u, and the pointer p itself, named *p, for a pointer p.
func locateUintptr(addr ast.Expr) (ast.Expr, string) {
	if u, ok := addr.(*ast.UnaryExpr); ok && u.Op == token.AND {
		return u.X, types.ExprString(u.X)
	}

	return addr, "*" + types.ExprString(addr)
}

// everyTerm reports whether t satisfies match or, for a type parameter, whether every type its
// constraint allows does. A constraint that lists no types, such as any, matches nothing: its
// instantiations are unknown.
func everyTerm(t types.Type, match func(types.Type) bool) bool {
	param, ok := t.(*types.TypeParam)
	if !ok {
		return match(t)
	}

	n, ok := countMatchingTerms(param.Constraint(), match)

	return ok && n > 0
}

// countMatchingTerms counts the types a constraint lists, through unions and embedded
// constraints, and reports false as soon as one of them fails match.
func countMatchingTerms(constraint types.Type, match func(types.Type) bool) (int, bool) {
	iface, ok := constraint.Underlying().(*types.Interface)
	if !ok {
		return 1, match(constraint)
	}

	n := 0

	for embedded := range iface.EmbeddedTypes() {
		switch embedded := embedded.(type) {
		case *types.Union:
			for term := range embedded.Terms() {
				if !match(term.Type()) {
					return 0, false
				}

				n++
			}
		default:
			m, ok := countMatchingTerms(embedded, match)
			if !ok {
				return 0, false
			}

			n += m
		}
	}

	return n, true
}
