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

package sharing

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/tochemey/gomemsafe/internal/flow"
)

const syncsSrc = `package p

import "sync"

type waiter interface{ Wait() }

func waits(wg *sync.WaitGroup) { wg.Wait() }

func plain() {}

func channels(ch chan int) {
	<-ch
	ch <- 1
	select {
	case <-ch:
	default:
	}
}

func notReceive(n int) int { return -n }

func calls(w waiter, f func(), wg *sync.WaitGroup, b []byte) {
	w.Wait()
	f()
	waits(wg)
	plain()
	_ = append(b, 0)
	func() { wg.Wait() }()
	func() { plain() }()
}

func deferred(wg *sync.WaitGroup) {
	defer plain()
}

func body(wg *sync.WaitGroup) {
	n := 0
	go func() {
		n = 1
		wg.Wait()
		n = 2
		if n > 0 {
			return
		}
		n = 3
	}()
	_ = n
}

func phiOnePath(c bool, wg *sync.WaitGroup) {
	n := 0
	go func() {
		v := 0
		if c {
			wg.Wait()
			v = 1
		}
		n = v
	}()
	_ = n
}

func phiBothPaths(c bool, wg *sync.WaitGroup) {
	n := 0
	go func() {
		v := 0
		if c {
			wg.Wait()
			v = 1
		} else {
			wg.Wait()
		}
		n = v
	}()
	_ = n
}

func noAccess() {
	n := 0
	go func() { _ = &n }()
}
`

// stubSyncs is a stub of the summary's Syncs: sync.WaitGroup.Wait synchronises, and so does the
// function named waits, which calls it.
func stubSyncs(fn *types.Func) bool {
	return fn.Name() == "waits" || fn.FullName() == "(*sync.WaitGroup).Wait"
}

func TestSynchronizesChannelOperations(t *testing.T) {
	pkg := buildSSA(t, syncsSrc)
	s := newSyncer(stubSyncs)

	for _, instr := range instrsOf[*ssa.UnOp](pkg.Func("channels")) {
		if !s.synchronizes(instr) {
			t.Errorf("%v does not synchronise; a receive does", instr)
		}
	}

	if !s.synchronizes(instrOf[*ssa.Send](t, pkg.Func("channels"))) || !s.synchronizes(instrOf[*ssa.Select](t, pkg.Func("channels"))) {
		t.Error("a send or a select does not synchronise")
	}

	if s.synchronizes(instrOf[*ssa.UnOp](t, pkg.Func("notReceive"))) {
		t.Error("a negation synchronises")
	}

	if !s.synchronizes(instrOf[*ssa.RunDefers](t, pkg.Func("deferred"))) {
		t.Error("running deferred calls does not synchronise")
	}
}

// TestSynchronizesCalls covers each kind of call in calls, in source order: an interface method,
// a function value, a function whose summary synchronises, one whose summary does not, a builtin,
// and function literals called where they are made, with and without synchronisation.
func TestSynchronizesCalls(t *testing.T) {
	pkg := buildSSA(t, syncsSrc)
	s := newSyncer(stubSyncs)

	want := []bool{true, true, true, false, false, true, false}

	callInstrs := instrsOf[*ssa.Call](pkg.Func("calls"))
	if len(callInstrs) != len(want) {
		t.Fatalf("calls has %d calls, want %d", len(callInstrs), len(want))
	}

	for i, call := range callInstrs {
		if got := s.synchronizes(call); got != want[i] {
			t.Errorf("call %d, %v: synchronizes = %v, want %v", i, call, got, want[i])
		}
	}

	// Each callee is decided once: waits, plain, the two literals, and WaitGroup.Wait, which the
	// first literal calls. Asking again reads what was remembered, without the stub.
	if len(s.decided) != 5 {
		t.Errorf("remembered %d callees, want 5", len(s.decided))
	}

	s.syncs = func(*types.Func) bool {
		t.Error("a decided callee was looked up again")
		return false
	}

	for fn, syncs := range s.decided {
		if s.function(fn) != syncs {
			t.Errorf("%s: a second look disagrees with the first", fn)
		}
	}
}

// TestUnsynchronizedKeepsAccessesBeforeSynchronising covers a goroutine that writes n before it
// waits, and twice after: only the first write may race. It reads its other free variable, wg,
// before the wait too.
func TestUnsynchronizedKeepsAccessesBeforeSynchronising(t *testing.T) {
	pkg := buildSSA(t, syncsSrc)
	body := pkg.Func("body").AnonFuncs[0]

	var eng flow.Engine

	n := -1

	for i, fv := range body.FreeVars {
		if fv.Name() == "n" {
			n = i
		}
	}

	racing := newSyncer(stubSyncs).unsynchronized(body, &eng)
	if len(racing) != 2 || len(racing[n]) != 1 || !racing[n][0].write || len(racing[1-n]) != 1 || racing[1-n][0].write {
		t.Fatalf("racing = %+v, want the write of n and the read of wg before the wait", racing)
	}

	eng.Budget = 1

	if racing := newSyncer(stubSyncs).unsynchronized(body, &eng); len(racing) != 2 || len(racing[0])+len(racing[1]) != 0 {
		t.Fatalf("racing over budget = %+v, want none", racing)
	}

	if racing := newSyncer(stubSyncs).unsynchronized(pkg.Func("noAccess").AnonFuncs[0], &eng); len(racing) != 1 || len(racing[0]) != 0 {
		t.Fatalf("racing without an access = %+v, want none", racing)
	}
}

// TestUnsynchronizedMeetsPathsAtPhis covers goroutines whose paths merge at a phi before they
// write n: synchronising on one path leaves the write racing, on both paths it does not.
func TestUnsynchronizedMeetsPathsAtPhis(t *testing.T) {
	pkg := buildSSA(t, syncsSrc)

	for name, want := range map[string]int{"phiOnePath": 1, "phiBothPaths": 0} {
		var eng flow.Engine

		body := pkg.Func(name).AnonFuncs[0]
		if len(instrsOf[*ssa.Phi](body)) == 0 {
			t.Fatalf("%s: the goroutine has no phi", name)
		}

		racing := newSyncer(stubSyncs).unsynchronized(body, &eng)

		writes := 0

		for i, fv := range body.FreeVars {
			if fv.Name() == "n" {
				writes = len(racing[i])
			}
		}

		if writes != want {
			t.Errorf("%s: %d racing writes of n, want %d", name, writes, want)
		}
	}
}

func buildSSA(t *testing.T, src string) *ssa.Package {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "p.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}

	pkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset,
		types.NewPackage("p", "p"), []*ast.File{file}, ssa.BuilderMode(0))
	if err != nil {
		t.Fatal(err)
	}

	return pkg
}

// instrOf returns fn's first instruction of type T.
func instrOf[T ssa.Instruction](t *testing.T, fn *ssa.Function) T {
	t.Helper()

	all := instrsOf[T](fn)
	if len(all) == 0 {
		t.Fatalf("%s has no %T", fn, *new(T))
	}

	return all[0]
}

// instrsOf returns fn's instructions of type T, in block order.
func instrsOf[T ssa.Instruction](fn *ssa.Function) []T {
	var out []T

	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			if x, ok := instr.(T); ok {
				out = append(out, x)
			}
		}
	}

	return out
}
