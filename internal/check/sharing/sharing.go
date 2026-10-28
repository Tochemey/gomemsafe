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

// Package sharing is the family of Tier 1 checks on variables shared between goroutines, MS401
// and MS402. MS401 is implemented.
//
// MS401 is a variable that a goroutine's function literal captures, written on one side and read
// or written on the other, with nothing between the two accesses that may order them. The two
// sides are the goroutine and the function that starts it, after the go statement, which orders
// everything before it. A pair of accesses is reported when some path from the go statement
// reaches the starting function's access without synchronising, and some path from the
// goroutine's start reaches its access without synchronising, so that neither side can have
// waited for the other. What synchronises is in file syncs.go, and the dataflow over the starting
// function in races.go.
//
// Synchronising is a channel operation, a select, a call the Go memory model documents as
// completing a happens-before edge, such as sync.WaitGroup.Wait or Mutex.Lock, a call whose
// summary says it may synchronise, such as errgroup.Group.Wait, and a call of an interface method
// or a function value, which may do either. The check does not match a release to its acquire:
// any of these, on either side, keeps the pair silent. Every choice errs towards silence, because
// a Tier 1 finding must almost always be a real race.
//
// An access is a load or a store of the variable itself, or of a field of a struct variable, so
// that the goroutine's s.a and the starting function's s.b are told apart. The check does not
// follow the variable's address passed on, a write through a pointer the variable holds, a
// variable captured only by a function literal nested in the goroutine, a goroutine started as
// go f(&x), or two goroutines racing with each other.
package sharing

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/catalog"
	"github.com/tochemey/gomemsafe/internal/directive"
	"github.com/tochemey/gomemsafe/internal/flow"
	"github.com/tochemey/gomemsafe/internal/lazyssa"
	"github.com/tochemey/gomemsafe/internal/prefilter"
	"github.com/tochemey/gomemsafe/internal/report"
	"github.com/tochemey/gomemsafe/internal/summary"
	"github.com/tochemey/gomemsafe/internal/trace"
	"github.com/tochemey/gomemsafe/internal/vetmode"
)

// CaptureID is the check ID of a variable a goroutine captures and accesses with no
// synchronisation.
const CaptureID = catalog.UnsynchronizedCapture

// Diagnostic texts of MS401. The variable's name is the first %s after the ID.
const (
	msgRace          = "%s: %s %s here while a goroutine %s it, with no synchronisation between them"
	msgGoroutineSide = "%s %s in the goroutine here"
	msgStarted       = "goroutine started here"
	helpRace         = "wait for the goroutine before this access, for example with sync.WaitGroup.Wait or " +
		"a channel receive, or guard %s with a mutex on both sides"
)

// budget is the dataflow budget per function; zero means flow.DefaultBudget. Tests lower it to
// see what a function over budget does: it reports nothing.
var budget int

// Analyzer is the sharing check.
var Analyzer = &analysis.Analyzer{
	Name: "sharing",
	Doc: "reports variables a goroutine captures that are written on one side and accessed on the " +
		"other with no synchronisation between them (MS401)",
	Requires: []*analysis.Analyzer{
		directive.Analyzer, prefilter.Analyzer, lazyssa.Analyzer, summary.Analyzer,
	},
	Run: run,
}

type checker struct {
	pass *analysis.Pass
	sync *syncer
	eng  flow.Engine
}

// run returns at once in facts-only runs: unitchecker runs sharing on every dependency only
// because it requires the summary analyzer. Only functions with a go statement are lowered to
// SSA.
func run(pass *analysis.Pass) (any, error) {
	if vetmode.FactsOnly() {
		trace.Record(pass.Pkg.Path(), pass.Analyzer.Name, trace.KindSkipped, 0)
		return nil, nil
	}

	pre := pass.ResultOf[prefilter.Analyzer].(*prefilter.Result)

	var funcs []*prefilter.Func

	for _, f := range pre.Funcs {
		if startsGoroutine(f.Decl) {
			funcs = append(funcs, f)
		}
	}

	if len(funcs) == 0 {
		return nil, nil
	}

	ssaRes := pass.ResultOf[lazyssa.Analyzer].(*lazyssa.Result)
	c := &checker{
		pass: pass,
		sync: newSyncer(pass.ResultOf[summary.Analyzer].(*summary.Result).Syncs),
		eng:  flow.Engine{Budget: budget},
	}

	for _, f := range funcs {
		if fn := ssaRes.Func(f.Obj); fn != nil {
			c.function(fn)
		}
	}

	for range c.eng.BudgetHits {
		trace.Record(pass.Pkg.Path(), pass.Analyzer.Name, trace.KindBudget, 0)
	}

	return nil, nil
}

// function reports the races of the goroutines fn starts, and of those every function literal
// nested in it starts.
func (c *checker) function(fn *ssa.Function) {
	if r := newRaces(fn, c.sync, &c.eng); len(r.pairs) > 0 {
		if sol, outcome := c.eng.Solve(fn, r); outcome == flow.Solved {
			reported := make([]bool, len(r.pairs))
			entry := fn.Blocks[0].Instrs[0]

			sol.Walk(func(instr ssa.Instruction, state *flow.Bitset) {
				// No goroutine runs before the function's first instruction, which no edge leads
				// back to; the state before it does not yet hold that.
				if instr != entry {
					c.reportRaces(r, instr, state, reported)
				}
			})
		}
	}

	for _, anon := range fn.AnonFuncs {
		c.function(anon)
	}
}

// reportRaces reports an access instr makes to a variable of r where a goroutine that captures
// it may still run unsynchronised and makes a conflicting access. Each access is reported once,
// with the first such goroutine, and each goroutine's pair at most once, at its first access:
// several goroutines racing at one access are one finding.
func (c *checker) reportRaces(r *races, instr ssa.Instruction, state *flow.Bitset, reported []bool) {
	for _, a := range r.accesses[instr] {
		first, other, found := pair{}, access{}, false

		for _, k := range r.byVar[a.v] {
			if reported[k] || state.Has(k) {
				continue
			}

			if conflicting, ok := r.pairs[k].conflict(a); ok {
				reported[k] = true

				if !found {
					first, other, found = r.pairs[k], conflicting, true
				}
			}
		}

		if found {
			c.report(first, a, other)
		}
	}
}

// report reports the access a of the starting function, racing with the goroutine's access other.
// An access SSA gives no position is placed at the go statement.
func (c *checker) report(p pair, a, other access) {
	name := variableName(p.v, a.path)
	start := p.start.Pos()
	goroutineSide := fmt.Sprintf(msgGoroutineSide, variableName(p.v, other.path), participle(other.write))

	report.Report(c.pass, analysis.Diagnostic{
		Pos:      cmp.Or(a.instr.Pos(), start),
		Category: CaptureID.String(),
		Message:  fmt.Sprintf(msgRace, CaptureID, name, participle(a.write), verb(other.write)),
		Related: []analysis.RelatedInformation{
			{Pos: cmp.Or(other.instr.Pos(), start), Message: goroutineSide},
			{Pos: start, Message: msgStarted},
		},
		SuggestedFixes: []analysis.SuggestedFix{{Message: fmt.Sprintf(helpRace, name)}},
	})
}

// startsGoroutine reports whether decl's body has a go statement. Only such functions are
// lowered to SSA.
func startsGoroutine(decl *ast.FuncDecl) bool {
	found := false

	ast.Inspect(decl.Body, func(n ast.Node) bool {
		_, isGo := n.(*ast.GoStmt)
		found = found || isGo

		return !found
	})

	return found
}

// variableName names what an access with path reaches in the variable at the address v, an
// *ssa.Alloc or an *ssa.FreeVar, both pointers: the variable's name, followed by the name of each
// field on the path.
func variableName(v ssa.Value, path []int) string {
	name := v.Name()
	if alloc, ok := v.(*ssa.Alloc); ok && alloc.Comment != "" {
		name = alloc.Comment
	}

	t := v.Type().Underlying().(*types.Pointer).Elem()

	for _, i := range path {
		st, ok := t.Underlying().(*types.Struct)
		if !ok || i >= st.NumFields() {
			break
		}

		name += "." + st.Field(i).Name()
		t = st.Field(i).Type()
	}

	return name
}

// participle words an access in the starting function.
func participle(write bool) string {
	if write {
		return "written"
	}

	return "read"
}

// verb words an access in the goroutine.
func verb(write bool) string {
	if write {
		return "writes"
	}

	return "reads"
}
