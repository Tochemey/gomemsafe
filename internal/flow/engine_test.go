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

package flow

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// callProblem is a test problem over facts named by calls: set(i) adds fact i, kill(i)
// removes it. check(i) calls record whether fact i holds, keyed by the source line.
type callProblem struct {
	n     int
	fset  *token.FileSet
	calls map[ssa.Instruction]namedCall
}

// namedCall is a call to set, kill, or check with its constant argument.
type namedCall struct {
	name string
	arg  int
}

func TestSolveIsMustAnalysis(t *testing.T) {
	fn := buildFunc(t, `
func f(c bool) {
	set(0)
	set(1)
	if c {
		kill(0)
	}
	check(0) // line 9: killed on one path
	check(1) // line 10: holds on both paths
}`)

	got := solveChecks(t, fn, 2)
	if got[9] || !got[10] {
		t.Fatalf("checks = %v, want line 9 false and line 10 true", got)
	}
}

func TestSolveReachesLoopFixpoint(t *testing.T) {
	fn := buildFunc(t, `
func f(n int) {
	set(0)
	for i := 0; i < n; i++ {
		check(0) // line 6: killed on the back edge, so not on every path
		kill(0)
	}
	check(0) // line 9: not killed when the loop never runs, but killed when it does
}`)

	got := solveChecks(t, fn, 1)
	if got[6] || got[9] {
		t.Fatalf("checks = %v, want both false", got)
	}
}

func TestSolveNestedLoops(t *testing.T) {
	fn := buildFunc(t, `
func f(n int) {
	for i := 0; i < n; i++ {
		set(0)
		for j := 0; j < n; j++ {
			check(0) // line 7: set on every path into the inner loop
		}
	}
}`)

	if got := solveChecks(t, fn, 1); !got[7] {
		t.Fatalf("checks = %v, want line 7 true", got)
	}
}

func TestSolveSkipsUnreachableBlocks(t *testing.T) {
	fn := buildFunc(t, `
func f() {
	set(0)
	return
	check(0) // line 6: unreachable
}`)

	if got := solveChecks(t, fn, 1); len(got) != 0 {
		t.Fatalf("checks = %v, want no visit of the unreachable block", got)
	}
}

func TestSolveStopsAtBudget(t *testing.T) {
	fn := buildFunc(t, `
func f(n int) {
	for i := 0; i < n; i++ {
		set(0)
	}
}`)

	eng := Engine{Budget: 1}

	if _, outcome := eng.Solve(fn, newCallProblem(fn, 1)); outcome != Unknown {
		t.Fatalf("outcome = %v, want Unknown", outcome)
	}

	if eng.BudgetHits != 1 {
		t.Fatalf("BudgetHits = %d, want 1", eng.BudgetHits)
	}
}

func TestSolveLargeFunctionIsFast(t *testing.T) {
	var src strings.Builder

	src.WriteString("func f(c bool) {\n")

	for range 10_000 {
		src.WriteString("\tif c {\n\t\tset(0)\n\t}\n")
	}

	src.WriteString("}\n")

	fn := buildFunc(t, src.String())

	var eng Engine

	if _, outcome := eng.Solve(fn, newCallProblem(fn, 1)); outcome != Solved {
		t.Fatalf("outcome = %v, want Solved", outcome)
	}
}

func TestSolveAllocatesNothingWhenReused(t *testing.T) {
	fn := buildFunc(t, `
func f(c bool) {
	set(0)
	if c {
		kill(0)
	}
	check(0)
}`)

	var eng Engine

	p := newCallProblem(fn, 1)
	eng.Solve(fn, p) // size the buffers

	allocs := testing.AllocsPerRun(100, func() { eng.Solve(fn, p) })
	if allocs != 0 {
		t.Fatalf("Solve allocated %v times per run, want 0", allocs)
	}
}

func TestWalkWithoutSolutionDoesNothing(t *testing.T) {
	var s Solution

	s.Walk(func(ssa.Instruction, *Bitset) { t.Fatal("visited an instruction") })
}

// BenchmarkSolveLarge solves a function of more than 50,000 instructions, which must take at
// most 50 ms.
func BenchmarkSolveLarge(b *testing.B) {
	var src strings.Builder

	src.WriteString("func f(c bool) {\n")

	for range 17_000 { // three SSA instructions per branch
		src.WriteString("\tif c {\n\t\tset(0)\n\t}\n")
	}

	src.WriteString("}\n")

	fn := buildFunc(b, src.String())

	instrs := 0
	for _, blk := range fn.Blocks {
		instrs += len(blk.Instrs)
	}

	if instrs < 50_000 {
		b.Fatalf("the function has %d instructions, want at least 50,000", instrs)
	}

	var eng Engine

	p := newCallProblem(fn, 1)
	eng.Solve(fn, p) // the first solve sizes the buffers; steady state reuses them

	b.ReportAllocs()

	for b.Loop() {
		if _, outcome := eng.Solve(fn, p); outcome != Solved {
			b.Fatalf("outcome = %v, want Solved", outcome)
		}
	}
}

func BenchmarkSolve(b *testing.B) {
	fn := buildFunc(b, `
func f(n int, c bool) {
	set(0)
	for i := 0; i < n; i++ {
		if c {
			kill(0)
		} else {
			set(1)
		}
	}
	check(0)
}`)

	var eng Engine

	p := newCallProblem(fn, 2)

	b.ReportAllocs()

	for b.Loop() {
		eng.Solve(fn, p)
	}
}

func (p *callProblem) N() int { return p.n }

func (p *callProblem) Phi(*ssa.Phi, func(int) *Bitset, *Bitset) {}

func (p *callProblem) Transfer(instr ssa.Instruction, state *Bitset) {
	c, ok := p.calls[instr]
	if !ok {
		return
	}

	switch c.name {
	case "set":
		state.Set(c.arg)
	case "kill":
		state.Clear(c.arg)
	}
}

// newCallProblem indexes the set, kill, and check calls of fn.
func newCallProblem(fn *ssa.Function, n int) *callProblem {
	p := &callProblem{n: n, fset: fn.Prog.Fset, calls: make(map[ssa.Instruction]namedCall)}

	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			if name, arg, ok := callArg(instr); ok {
				p.calls[instr] = namedCall{name: name, arg: arg}
			}
		}
	}

	return p
}

// buildFunc type-checks src as the body of package p, with set, kill, and check declared,
// and returns the SSA form of the function named f.
func buildFunc(tb testing.TB, src string) *ssa.Function {
	tb.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "p.go", "package p\n"+src+"\nfunc set(int)\nfunc kill(int)\nfunc check(int)\n", parser.SkipObjectResolution)
	if err != nil {
		tb.Fatal(err)
	}

	pkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset,
		types.NewPackage("p", "p"), []*ast.File{file}, ssa.BuilderMode(0))
	if err != nil {
		tb.Fatal(err)
	}

	return pkg.Func("f")
}

// solveChecks solves a callProblem over fn and returns, per source line of each check(i)
// call, whether fact i held there.
func solveChecks(t *testing.T, fn *ssa.Function, n int) map[int]bool {
	t.Helper()

	var eng Engine

	p := newCallProblem(fn, n)

	sol, outcome := eng.Solve(fn, p)
	if outcome != Solved {
		t.Fatalf("outcome = %v, want Solved", outcome)
	}

	got := make(map[int]bool)

	sol.Walk(func(instr ssa.Instruction, state *Bitset) {
		if c, ok := p.calls[instr]; ok && c.name == "check" {
			got[p.fset.Position(instr.Pos()).Line] = state.Has(c.arg)
		}
	})

	return got
}

// callArg returns the callee name and constant argument of a call to set, kill, or check.
func callArg(instr ssa.Instruction) (string, int, bool) {
	call, ok := instr.(*ssa.Call)
	if !ok {
		return "", 0, false
	}

	callee := call.Common().StaticCallee()
	if callee == nil || len(call.Common().Args) != 1 {
		return "", 0, false
	}

	c, ok := call.Common().Args[0].(*ssa.Const)
	if !ok {
		return "", 0, false
	}

	return callee.Name(), int(c.Int64()), true
}
