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

package escape

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

	"github.com/tochemey/gomemsafe/internal/effect"
)

// escapesSrc declares the functions under test. The fake lookup treats keeps and run as
// keeping their argument, gives as returning its argument's memory, and takes as consuming it.
const escapesSrc = `package p

import (
	"fmt"
	"io"
	"sort"
	"unsafe"
)

type T struct {
	next *T
	n    int
	buf  []byte
}

type S struct{ t *T }

type Holder struct {
	t     *T
	s     S
	m     map[string]*T
	items []*T
	buf   []byte
	f     func()
}

type Sink interface{ Put(*T) }

var global *T

var globals []*T

var count int

func (t *T) Method() {}

func keeps(t *T) { _ = t }

func run(f func()) { _ = f }

func gives(t *T) *T { return t }

func keepsAll(ts ...*T) { _ = ts }

func takes(t *T) { _ = t }

func reads(t *T) int { return t.n }

func field(h *Holder, t *T)   { h.t = t }
func element(h *Holder, t *T) { h.items[0] = t }
func entry(h *Holder, t *T)   { h.m["k"] = t }
func key(m map[*T]int, t *T)  { m[t] = 1 }
func global1(t *T)            { global = t }
func global2(t *T)            { globals[0] = t }
func pointer(pp **T, t *T)    { *pp = t }
func appended(h *Holder, t *T) { h.items = append(h.items, t) }
func appendedBytes(h *Holder, b []byte) { h.buf = append(h.buf, b...) }
func copied(dst, src []*T)    { copy(dst, src) }
func copiedBytes(dst, src []byte) { copy(dst, src) }
func sent(ch chan *T, t *T)   { ch <- t }
func selected(ch chan *T, t *T) {
	select {
	case ch <- t:
	default:
	}
}
func returned(t *T) *T      { return t }
func returnedNext(t *T) *T  { return t.next }
func returnedInt(t *T) int  { return t.n }
func started(t *T)          { go reads(t) }
func captured(t *T)         { go func() { _ = t.n }() }
func keeper(t *T)           { keeps(t) }
func consumer(t *T)         { takes(t) }
func unknownFunc(f func(*T), t *T) { f(t) }
func unknownMethod(s Sink, t *T)    { s.Put(t) }

type Contracts interface{ Contracted(a, b, c *T) }

func contractedBorrow(s Contracts, t *T)  { s.Contracted(t, nil, nil) }
func contractedConsume(s Contracts, t *T) { s.Contracted(nil, t, nil) }
func contractedOther(s Contracts, t *T)   { s.Contracted(nil, nil, t) }

type Lender interface{ Lends(a *T) *T }

func lent(s Lender, t *T) *T  { return s.Lends(t) }
func lentOnly(s Lender, t *T) { s.Lends(t) }
func knownMethod(r io.Reader, b []byte) { _, _ = r.Read(b) }
func goMethod(r io.Reader, b []byte)   { go r.Read(b) }
func panicked(t *T)         { panic(t) }
func fresh(h *Holder, t *T) {
	x := &T{}
	x.next = t
	h.t = x
}
func freshOnly(t *T) int {
	x := &T{}
	x.next = t
	return x.n
}
func variable(h *Holder, c bool, t *T) {
	var q *T
	p := &q
	_ = p
	if c {
		q = t
	}
	h.t = q
}
func throughCall(h *Holder, t *T) { h.t = gives(t) }
func throughRead(t *T)            { reads(t) }
func literalCalled(h *Holder, t *T)   { func() { h.t = t }() }
func literalDeferred(h *Holder, t *T) { defer func() { h.t = t }() }
func literalReturns(t *T) *T {
	f := func() *T { return t }
	return f()
}
func literalParam(h *Holder, t *T) { func(x *T) { h.t = x }(t) }
func literalStored(h *Holder, t *T) { h.f = func() { _ = t } }
func literalPassed(t *T)            { run(func() { _ = t }) }
func literalReads(t *T) int         { return func() int { return t.n }() }
func bound(h *Holder, t *T)         { h.f = t.Method }
func methodExpr(t *T) {
	f := (*T).Method
	f(t)
}
func rangeMap(h *Holder, m map[string]*T) {
	for _, v := range m {
		h.t = v
	}
}
func rangeSlice(h *Holder, items []*T) {
	for _, v := range items {
		h.t = v
	}
}
func received(h *Holder, ch chan *T) { h.t = <-ch }
func localChan(h *Holder, t *T) {
	ch := make(chan *T, 1)
	ch <- t
	h.t = <-ch
}
func selectReceived(h *Holder, ch chan *T, done chan struct{}) {
	select {
	case v := <-ch:
		h.t = v
	case <-done:
	}
}
func asserted(h *Holder, x any)  { h.t = x.(*T) }
func assertedOk(h *Holder, x any) {
	if t, ok := x.(*T); ok {
		h.t = t
	}
}
func looked(h *Holder, m map[string]*T) { h.t = m["k"] }
func lookedOk(h *Holder, m map[string]*T) {
	if t, ok := m["k"]; ok {
		h.t = t
	}
}
func byValue(h *Holder, s S)       { h.t = s.t }
func fromArray(h *Holder, a [2]*T) { h.t = a[0] }
func converted(h *Holder, p unsafe.Pointer) { h.t = (*T)(p) }
func stringified(b []byte) string  { return string(b) }
func sliced(h *Holder, b []*T)     { h.items = b[1:] }
func merged(h *Holder, c bool, a, b *T) {
	x := a
	if c {
		x = b
	}
	h.t = x
}
func plain(n int, s string) { count = n + len(s) }
func unsafeSliced(h *Holder, p *byte) { h.buf = unsafe.Slice(p, 1) }
func wrapped(err error) error { return fmt.Errorf("x: %w", err) }
func printed(t *T)           { fmt.Println(t) }
func sorted(items []*T)      { sort.Slice(items, func(i, j int) bool { return i < j }) }
func structStored(h *Holder, t *T) {
	var s S
	s.t = t
	h.s = s
}
func deferred(t *T)  { defer keeps(t) }
func selfStored(t *T) { t.next = t }
func intoOther(a, b *T) { a.next = b }
func twice(h *Holder, t *T) {
	var q *T
	p := &q
	_ = p
	q = t
	q = t
	h.t = q
}
func freshReturned(h *Holder) {
	x := &T{}
	f := func() *T { return x }
	h.t = f()
}
func goFunc(f func())             { go f() }
func copiedInto(dst, src []*T)    { copy(dst[1:], src) }
func copiedIntoGlobal(src []*T)   { copy(globals[1:], src) }
func variadic(t *T)                { keepsAll(nil, t) }
func spread(items []*T)            { keepsAll(items[1:]...) }
func variadicNumbers(n int)        { fmt.Println(n) }
`

// fakeLookup answers for the fixture's helpers by name, and for the standard library from the
// known effects, as the summary analyzer's Result does.
type fakeLookup struct{}

func (fakeLookup) Retains(fn *types.Func) effect.ArgSet {
	if retains, _, known := effect.Kept(fn); known {
		return retains
	}

	if fn.Name() == "keeps" || fn.Name() == "run" || fn.Name() == "keepsAll" {
		return 1 << 0
	}

	// The fact of an interface method whose contract borrows its first parameter and consumes its
	// second, with the receiver and the rest kept by default.
	if fn.Name() == "Contracted" {
		return 0b1101
	}

	// The fact of an interface method whose contract borrows its parameter, a view of which is
	// its result.
	if fn.Name() == "Lends" {
		return 0b01
	}

	return 0
}

func (fakeLookup) Returns(fn *types.Func) effect.ArgSet {
	if _, returns, known := effect.Kept(fn); known {
		return returns
	}

	if fn.Name() == "gives" {
		return 1 << 0
	}

	if fn.Name() == "Lends" {
		return 1 << 1
	}

	return 0
}

func (fakeLookup) Consumes(fn *types.Func) effect.ArgSet {
	if fn.Name() == "takes" {
		return 1 << 0
	}

	if fn.Name() == "Contracted" {
		return 1 << 2
	}

	return 0
}

// TestRetainedAndReturned checks, per function, which parameters' memory is kept beyond the
// call and which a result may share.
func TestRetainedAndReturned(t *testing.T) {
	pkg := buildEscapes(t, escapesSrc)

	for _, tc := range []struct {
		fn                 string
		retained, returned effect.ArgSet
	}{
		{"field", 1 << 1, 0},
		{"element", 1 << 1, 0},
		{"entry", 1 << 1, 0},
		{"key", 1 << 1, 0},
		{"global1", 1 << 0, 0},
		{"global2", 1 << 0, 0},
		{"pointer", 1 << 1, 0},
		{"appended", 1 << 1, 0},
		{"appendedBytes", 0, 0},
		{"copied", 1 << 1, 0},
		{"copiedBytes", 0, 0},
		{"sent", 1 << 1, 0},
		{"selected", 1 << 1, 0},
		{"returned", 0, 1 << 0},
		{"returnedNext", 0, 1 << 0},
		{"returnedInt", 0, 0},
		{"started", 1 << 0, 0},
		{"captured", 1 << 0, 0},
		{"keeper", 1 << 0, 0},
		{"consumer", 1 << 0, 0},
		{"unknownFunc", 1 << 1, 0},
		{"unknownMethod", 1<<0 | 1<<1, 0},
		{"knownMethod", 0, 0},
		{"goMethod", 1<<0 | 1<<1, 0},
		{"panicked", 1 << 0, 0},
		{"fresh", 1 << 1, 0},
		{"freshOnly", 0, 0},
		{"variable", 1 << 2, 0},
		{"throughCall", 1 << 1, 0},
		{"throughRead", 0, 0},
		{"literalCalled", 1 << 1, 0},
		{"literalDeferred", 1 << 1, 0},
		{"literalReturns", 0, 1 << 0},
		{"literalParam", 1 << 1, 0},
		{"literalStored", 1 << 1, 0},
		{"literalPassed", 1 << 0, 0},
		{"literalReads", 0, 0},
		{"bound", 1 << 1, 0},
		{"methodExpr", 0, 0},
		{"rangeMap", 1 << 1, 0},
		{"rangeSlice", 1 << 1, 0},
		{"received", 1 << 1, 0},
		{"localChan", 1 << 1, 0},
		{"selectReceived", 1 << 1, 0},
		{"asserted", 1 << 1, 0},
		{"assertedOk", 1 << 1, 0},
		{"looked", 1 << 1, 0},
		{"lookedOk", 1 << 1, 0},
		{"byValue", 1 << 1, 0},
		{"fromArray", 1 << 1, 0},
		{"converted", 1 << 1, 0},
		{"stringified", 0, 0},
		{"sliced", 1 << 1, 0},
		{"merged", 1<<2 | 1<<3, 0},
		{"plain", 0, 0},
		{"unsafeSliced", 1 << 1, 0},
		{"wrapped", 0, 1 << 0},
		{"printed", 0, 0},
		{"sorted", 0, 0},
		{"structStored", 1 << 1, 0},
		{"deferred", 1 << 0, 0},
		{"selfStored", 0, 0},
		{"intoOther", 1 << 1, 0},
		{"twice", 1 << 1, 0},
		{"freshReturned", 0, 0},
		{"goFunc", 1 << 0, 0},
		{"copiedInto", 1 << 1, 0},
		{"copiedIntoGlobal", 1 << 0, 0},
		{"variadic", 1 << 0, 0},
		{"spread", 1 << 0, 0},
		{"variadicNumbers", 0, 0},
	} {
		a := Analyze(pkg.Func(tc.fn), fakeLookup{})
		if a.Retained() != tc.retained || a.Returned() != tc.returned {
			t.Errorf("%s: retained %#x, returned %#x; want %#x, %#x", tc.fn, uint64(a.Retained()), uint64(a.Returned()),
				uint64(tc.retained), uint64(tc.returned))
		}
	}
}

// TestSharesLooksInsideStructsAndArrays checks which types carry memory: the pointer-like
// ones, and structs and arrays holding one, but not a type parameter or a struct of numbers.
func TestSharesLooksInsideStructsAndArrays(t *testing.T) {
	pkg := buildEscapes(t, escapesSrc).Pkg
	tp := types.NewTypeParam(types.NewTypeName(0, pkg, "P", nil), types.NewInterfaceType(nil, nil))
	numbers := types.NewStruct([]*types.Var{types.NewField(0, pkg, "n", types.Typ[types.Int], false)}, nil)

	for _, tc := range []struct {
		typ  types.Type
		want bool
	}{
		{pkg.Scope().Lookup("T").Type(), true},
		{types.NewPointer(numbers), true},
		{pkg.Scope().Lookup("S").Type(), true},
		{types.NewArray(pkg.Scope().Lookup("S").Type(), 2), true},
		{numbers, false},
		{types.NewArray(types.Typ[types.Int], 2), false},
		{types.Typ[types.String], false},
		{tp, false},
	} {
		if got := Shares(tc.typ); got != tc.want {
			t.Errorf("Shares(%s) = %v, want %v", tc.typ, got, tc.want)
		}
	}
}

// TestTooManyRootsKeepEverything covers a function whose parameters do not fit the set of
// roots: every memory-carrying parameter is taken to escape, and nothing is listed.
func TestTooManyRootsKeepEverything(t *testing.T) {
	var params []string
	for i := range maxRoots + 1 {
		params = append(params, "p"+string(rune('a'+i%26))+string(rune('a'+i/26))+" *int")
	}

	src := "package p\n\nfunc wide(" + strings.Join(params, ", ") + ", n int) *int { return nil }\n" +
		"func wideNoResult(" + strings.Join(params, ", ") + ", n int) {}\n"
	pkg := buildEscapes(t, src)

	a := Analyze(pkg.Func("wide"), fakeLookup{})
	if a.Retained() != 1<<maxRoots-1 || a.Returned() != 1<<maxRoots-1 || a.Escapes(0) != nil {
		t.Errorf("retained %#x, returned %#x, %d escapes; want every pointer parameter and no escapes",
			uint64(a.Retained()), uint64(a.Returned()), len(a.Escapes(0)))
	}

	if a := Analyze(pkg.Func("wideNoResult"), fakeLookup{}); a.Retained() != 1<<maxRoots-1 || a.Returned() != 0 {
		t.Errorf("no result: retained %#x, returned %#x; want every pointer parameter, and none returned",
			uint64(a.Retained()), uint64(a.Returned()))
	}
}

// TestWideLiteralKeepsWhatItCaptures covers a literal whose parameters and captured variables
// do not fit one analysis: what it captures escapes where it is made.
func TestWideLiteralKeepsWhatItCaptures(t *testing.T) {
	var params []string
	for i := range maxRoots {
		params = append(params, "p"+string(rune('a'+i%26))+string(rune('a'+i/26))+" int")
	}

	src := "package p\n\nfunc wide(t *int) {\n\t_ = func(" + strings.Join(params, ", ") + ") { _ = t }\n}\n"

	escapes := Analyze(buildEscapes(t, src).Func("wide"), fakeLookup{}).Escapes(0)
	if len(escapes) != 1 || escapes[0].Kind != CapturedByWideLiteral {
		t.Fatalf("escapes %+v, want one capture by a wide literal", escapes)
	}
}

// buildEscapes builds the SSA of src, a package p that may import the standard library.
func buildEscapes(t *testing.T, src string) *ssa.Package {
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
