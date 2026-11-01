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

package directive_test

import (
	"fmt"
	"go/ast"
	"go/types"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/tochemey/gomemsafe/internal/directive"
)

func TestReportsMalformedLockAnnotations(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), directive.Analyzer, "checklocks")
}

// TestGuardsResolveLocks checks the guards of the fixture's fields and globals: a lock in the
// struct, through a value or pointer field, promoted from an embedded struct, a global, or a
// field of a global, and the objects named as locks.
func TestGuardsResolveLocks(t *testing.T) {
	r := analysistest.Run(t, analysistest.TestData(), directive.Analyzer, "checklocks")[0]
	res := r.Result.(*directive.Result)
	pkg := r.Pass.Pkg

	global := func(name string) string { return pkg.Path() + "." + name }

	for _, tc := range []struct {
		obj  string
		want []directive.Guard
	}{
		{"first", []directive.Guard{{Name: "globalMu", Global: global("globalMu")}}},
		{"second", []directive.Guard{{Name: "globalMu", Global: global("globalMu")}}},
		{"trailing", []directive.Guard{{Name: "globalMu", Global: global("globalMu")}}},
		{"grouped", []directive.Guard{{Name: "globalMu", Global: global("globalMu")}}},
		{"nested", []directive.Guard{{Name: "globalBox.mu", Global: global("globalBox"), Path: []int{0}}}},
		{"twiceGlobal", []directive.Guard{{Name: "globalMu", Global: global("globalMu")}}},
		{"readOnGlobal", nil},
		{"inner", nil},
		{"headerFirst", []directive.Guard{{Name: "globalMu", Global: global("globalMu")}}},
		{"headerBoth", []directive.Guard{
			{Name: "globalMu", Global: global("globalMu")},
			{Name: "globalBox.mu", Global: global("globalBox"), Path: []int{0}},
		}},
		{"unresolved", nil},
		{"notALock", nil},
		{"guarded.byDoc", []directive.Guard{{Name: "mu", Path: []int{0}}}},
		{"guarded.byLine", []directive.Guard{{Name: "mu", Path: []int{0}}}},
		{"guarded.pair1", []directive.Guard{{Name: "mu", Path: []int{0}}}},
		{"guarded.pair2", []directive.Guard{{Name: "mu", Path: []int{0}}}},
		{"guarded.twoLocks", []directive.Guard{{Name: "mu", Path: []int{0}}, {Name: "rw", Path: []int{1}}}},
		{"guarded.viaValue", []directive.Guard{{Name: "in.mu", Path: []int{4, 0}}}},
		{"guarded.viaPointer", []directive.Guard{{Name: "inPtr.mu", Path: []int{5, 0}}}},
		{"guarded.viaEmbedded", []directive.Guard{{Name: "RWMutex", Path: []int{6, 0}}}},
		{"guarded.viaLocker", []directive.Guard{{Name: "locker", Path: []int{2}}}},
		{"guarded.viaPointerLock", []directive.Guard{{Name: "ptr", Path: []int{3}}}},
		{"guarded.viaGlobal", []directive.Guard{{Name: "globalMu", Global: global("globalMu")}}},
		{"guarded.viaGlobalField", []directive.Guard{{Name: "globalBox.mu", Global: global("globalBox"), Path: []int{0}}}},
		{"guarded.twice", []directive.Guard{{Name: "mu", Path: []int{0}}}},
		{"guarded.unresolved", nil},
		{"guarded.readOnField", nil},
		{"guarded.ignored", nil},
		{"guarded.marked", nil},
		{"anonymous.box.value", []directive.Guard{{Name: "mu", Path: []int{0}}}},
		{"generic.value", []directive.Guard{{Name: "mu", Path: []int{0}}}},
		{"globalBox.count", []directive.Guard{{Name: "mu", Path: []int{0}}}},
	} {
		got := res.GuardsOf(lookup(t, pkg, tc.obj))
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("GuardsOf(%s) = %+v, want %+v", tc.obj, got, tc.want)
		}
	}

	guarded, locks, contracted := res.LockFacts()

	if got := names(guarded); !strings.HasPrefix(got, "count,first,second,trailing,grouped,nested,headerFirst,headerBoth,twiceGlobal,byDoc,pair1,pair2,") {
		t.Errorf("guarded objects start with %s", got)
	}

	if got := names(locks); got != "mu,globalMu,mu,rw,mu,RWMutex,locker,ptr,mu,mu,mu,mu" {
		t.Errorf("locks = %s", got)
	}

	if got := names(contracted); got != "requires,requiresRead,mixed,acquires,releases,acquiresResults,requiresGlobals,ignored,onlyIgnored,twiceOnFunc,requiredAndExcluded,acquiresTwice,excludedAndRequired,excludesTwice,unnamedReceiver" {
		t.Errorf("contracted = %s", got)
	}

	for obj, want := range map[string]bool{"guarded.mu": true, "globalMu": true, "globalBox.mu": true, "guarded.byDoc": false, "first": false} {
		if got := res.IsLock(lookup(t, pkg, obj)); got != want {
			t.Errorf("IsLock(%s) = %v, want %v", obj, got, want)
		}
	}
}

// TestLockContracts checks the contracts of the fixture's functions: locks found in the
// receiver, a parameter, a result, or a global, in each mode, and none for a malformed one.
func TestLockContracts(t *testing.T) {
	r := analysistest.Run(t, analysistest.TestData(), directive.Analyzer, "checklocks")[0]
	res := r.Result.(*directive.Result)
	pkg := r.Pass.Pkg

	param := func(name string, index int, path ...int) directive.LockRef {
		return directive.LockRef{Name: name, Kind: directive.LockParam, Index: index, Path: path, Exclusive: true}
	}

	for _, tc := range []struct {
		fn   string
		want *directive.LockContract
	}{
		{"guarded.requires", &directive.LockContract{Requires: []directive.LockRef{param("g.mu", 0, 0)}}},
		{"guarded.requiresRead", &directive.LockContract{Requires: []directive.LockRef{
			{Name: "g.rw", Kind: directive.LockParam, Index: 0, Path: []int{1}},
		}}},
		{"guarded.mixed", &directive.LockContract{
			Requires: []directive.LockRef{param("g.mu", 0, 0), param("other.in.mu", 2, 4, 0),
				{Name: "other.rw", Kind: directive.LockParam, Index: 2, Path: []int{1}}},
			Excludes: []directive.LockRef{
				{Name: "g.rw", Kind: directive.LockParam, Index: 0, Path: []int{1}},
				{Name: "globalMu", Kind: directive.LockGlobal, Global: pkg.Path() + ".globalMu"},
			},
		}},
		{"guarded.acquires", &directive.LockContract{Acquires: []directive.LockRef{param("g.mu", 0, 0)}}},
		{"guarded.releases", &directive.LockContract{Releases: []directive.LockRef{param("g.mu", 0, 0)}}},
		{"acquiresResults", &directive.LockContract{Acquires: []directive.LockRef{
			{Name: "result.mu", Kind: directive.LockResult, Index: 0, Path: []int{0}, Exclusive: true},
			{Name: "second.in.mu", Kind: directive.LockResult, Index: 1, Path: []int{4, 0}, Exclusive: true},
		}}},
		{"requiresGlobals", &directive.LockContract{Requires: []directive.LockRef{
			{Name: "globalMu", Kind: directive.LockGlobal, Global: pkg.Path() + ".globalMu", Exclusive: true},
			{Name: "globalBox.mu", Kind: directive.LockGlobal, Global: pkg.Path() + ".globalBox", Path: []int{0}, Exclusive: true},
		}}},
		{"ignored", &directive.LockContract{Requires: []directive.LockRef{param("g.mu", 0, 0)}, Ignore: true}},
		{"onlyIgnored", &directive.LockContract{Ignore: true}},
		{"unresolvedParam", nil},
		{"requiresResult", nil},
		{"readsMutex", nil},
		{"twiceOnFunc", &directive.LockContract{Requires: []directive.LockRef{param("g.mu", 0, 0)}}},
		{"requiredAndExcluded", &directive.LockContract{Requires: []directive.LockRef{param("g.mu", 0, 0)}}},
		{"excludedAndRequired", &directive.LockContract{Excludes: []directive.LockRef{
			{Name: "g.mu", Kind: directive.LockParam, Index: 0, Path: []int{0}},
		}}},
		{"failOnFunc", nil},
		{"guarded.unnamedReceiver", &directive.LockContract{Requires: []directive.LockRef{param("mu", 1)}}},
		{"notStruct", nil},
		{"throughInt", nil},
		{"acquiresAbsent", nil},
		{"forcedFunc", nil},
		{"lines", nil},
	} {
		got, ok := res.LockContractOf(lookup(t, pkg, tc.fn).(*types.Func))
		if ok != (tc.want != nil) || fmt.Sprintf("%+v", got) != fmt.Sprintf("%+v", tc.want) {
			t.Errorf("LockContractOf(%s) = %+v, %v; want %+v", tc.fn, got, ok, tc.want)
		}
	}

	c, _ := res.LockContractOf(lookup(t, pkg, "guarded.mixed").(*types.Func))
	if len(c.HeldOnEntry()) != 3 || len(c.HeldOnExit()) != 3 || c.Empty() {
		t.Errorf("mixed: %d held on entry, %d on exit, empty %v", len(c.HeldOnEntry()), len(c.HeldOnExit()), c.Empty())
	}

	if !(*directive.LockContract)(nil).Empty() || !(&directive.LockContract{}).Empty() || (&directive.LockContract{Ignore: true}).Empty() {
		t.Error("Empty is wrong for a nil, an empty, or an ignoring contract")
	}
}

// TestForcedLinesAreThoseWithForce checks the lines of the fixture's last function: a force
// forces the assertion on its line, an ignore suppresses MS403 there, checklocks' test marker
// does nothing, and a misplaced or an unsupported annotation is reported.
func TestForcedLinesAreThoseWithForce(t *testing.T) {
	r := analysistest.Run(t, analysistest.TestData(), directive.Analyzer, "checklocks")[0]
	res := r.Result.(*directive.Result)

	var body *ast.BlockStmt

	for _, decl := range r.Pass.Files[0].Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "lines" {
			body = fn.Body
		}
	}

	var got []bool

	for _, stmt := range body.List[1:] {
		got = append(got, res.Forced(r.Pass.Fset, stmt.Pos()))
	}

	if want := []bool{true, false, false, false, false}; !slices.Equal(got, want) {
		t.Errorf("Forced on the lines of lines = %v, want %v", got, want)
	}

	if !res.Suppressed(r.Pass.Fset, body.List[2].Pos(), "MS403") || res.Suppressed(r.Pass.Fset, body.List[1].Pos(), "MS403") {
		t.Error("+checklocksignore on a line does not suppress MS403 there, or a force does")
	}
}

// lookup finds the object name names in pkg: a package-level object, or "type.field" for a
// field, through a pointer or an anonymous struct, or "type.method" for a method.
func lookup(t *testing.T, pkg *types.Package, name string) types.Object {
	t.Helper()

	parts := strings.Split(name, ".")

	obj := pkg.Scope().Lookup(parts[0])
	if obj == nil {
		t.Fatalf("%s: no %s in the package", name, parts[0])
	}

	for _, part := range parts[1:] {
		var found types.Object

		if named, ok := obj.Type().(*types.Named); ok {
			for i := range named.NumMethods() {
				if named.Method(i).Name() == part {
					found = named.Method(i)
				}
			}
		}

		if found == nil {
			st, ok := obj.Type().Underlying().(*types.Struct)
			if ptr, isPtr := obj.Type().Underlying().(*types.Pointer); isPtr {
				st, ok = ptr.Elem().Underlying().(*types.Struct)
			}

			if !ok {
				t.Fatalf("%s: %s is not a struct", name, obj.Name())
			}

			for i := range st.NumFields() {
				if st.Field(i).Name() == part {
					found = st.Field(i)
				}
			}
		}

		if found == nil {
			t.Fatalf("%s: no %s in %s", name, part, obj.Name())
		}

		obj = found
	}

	return obj
}

// names joins the names of objects.
func names[T types.Object](objs []T) string {
	var out []string
	for _, obj := range objs {
		out = append(out, obj.Name())
	}

	return strings.Join(out, ",")
}
