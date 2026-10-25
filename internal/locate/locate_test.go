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

package locate

import (
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"
)

// src uses the parameter p in every kind of instruction SSA places away from it.
const src = `package p

type box struct{ f *int }

type T struct{}

func (*T) M(*int) {}

func use(...any) {}

func f(p *int, t *T, m map[*int]int, ch chan *int, ps []*int) *int {
	use(p)
	t.M(p)
	(*T).M(t, p)
	_ = *p
	_ = p == nil
	_ = m[p]
	m[p] = 1
	ch <- p
	b := box{f: p}
	b.f = p
	ps[0] = p
	select {
	case ch <- p:
	default:
	}
	go use(p)
	defer use(p)
	return p
}
`

// TestOperandIsTheValueAsWritten checks every instruction that uses the parameter p: the
// operand found is p itself, written where the source writes it.
func TestOperandIsTheValueAsWritten(t *testing.T) {
	l, fn := build(t)
	p := fn.Params[0]

	found := 0

	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			if isAlias(instr) || !usesValue(instr, p) {
				continue
			}

			expr := l.Operand(instr, p)
			if expr == nil {
				t.Errorf("%s: no operand found", instr)
				continue
			}

			if text := types.ExprString(expr); text != "p" {
				t.Errorf("%s: operand %s, want p", instr, text)
			}

			if pos := l.UsePos(instr, p, token.NoPos); pos != expr.Pos() {
				t.Errorf("%s: UsePos %v, want the operand's %v", instr, pos, expr.Pos())
			}

			found++
		}
	}

	// Every statement but the three calls of use, which pass p inside their variadic slice.
	if found != 12 {
		t.Fatalf("found %d uses of p, want 12", found)
	}
}

// TestFallsBackWithoutSource covers an instruction with no source call or position: there is no
// operand, and UsePos falls back.
func TestFallsBackWithoutSource(t *testing.T) {
	l := New(&analysis.Pass{}, make(map[token.Pos]*ast.CallExpr))
	call := &ssa.Call{}

	if expr := l.Operand(call, nil); expr != nil {
		t.Errorf("Operand = %v, want nil", expr)
	}

	if pos := l.UsePos(call, nil, 42); pos != 42 {
		t.Errorf("UsePos = %v, want the fallback", pos)
	}

	v := ssa.NewConst(constant.MakeInt64(1), types.Typ[types.Int])
	if expr := argExpr(&ast.CallExpr{Fun: ast.NewIdent("f")}, []ssa.Value{v}, v, 0); expr != nil {
		t.Errorf("argExpr = %v, want nil for an argument SSA adds", expr)
	}
}

func TestReceiverIsOnlyAnExplicitMethodReceiver(t *testing.T) {
	l, fn := build(t)

	var receivers []string

	for _, call := range l.calls {
		if recv, ok := l.Receiver(call); ok {
			receivers = append(receivers, types.ExprString(recv))
		}
	}

	// t.M(p) has t as its receiver; (*T).M(t, p) is a method expression, and calls of use have
	// none.
	if strings.Join(receivers, ",") != "t" {
		t.Fatalf("%s: receivers %v, want only t", fn, receivers)
	}
}

// build type-checks and lowers src, returning a Locator for it and its function f.
func build(t *testing.T) (*Locator, *ssa.Function) {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "p.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}

	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue), Defs: make(map[*ast.Ident]types.Object),
		Uses: make(map[*ast.Ident]types.Object), Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}

	pkg, err := (&types.Config{Importer: importer.Default()}).Check("p", fset, []*ast.File{file}, info)
	if err != nil {
		t.Fatal(err)
	}

	prog := ssa.NewProgram(fset, ssa.BuilderMode(0))
	ssaPkg := prog.CreatePackage(pkg, []*ast.File{file}, info, false)
	ssaPkg.Build()

	calls := make(map[token.Pos]*ast.CallExpr)

	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			calls[call.Lparen] = call
		}

		return true
	})

	pass := &analysis.Pass{Fset: fset, Files: []*ast.File{file}, TypesInfo: info, Pkg: pkg}

	return New(pass, calls), ssaPkg.Func("f")
}

// isAlias reports whether instr only gives its operand another type, which checks do not count
// as a use, or is a debug reference.
func isAlias(instr ssa.Instruction) bool {
	switch instr.(type) {
	case *ssa.MakeInterface, *ssa.ChangeType, *ssa.DebugRef:
		return true
	}

	return false
}

func usesValue(instr ssa.Instruction, v ssa.Value) bool {
	var ops [8]*ssa.Value

	for _, op := range instr.Operands(ops[:0]) {
		if op != nil && *op == v {
			return true
		}
	}

	return false
}
