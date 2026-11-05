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
	"slices"
	"testing"

	"golang.org/x/tools/go/ssa"
)

// TestEscapesSayHowAndWhere checks the kind of each escape of one parameter, in source order,
// and, for an escape into a callee, which callee and argument.
func TestEscapesSayHowAndWhere(t *testing.T) {
	pkg := buildEscapes(t, escapesSrc)

	for _, tc := range []struct {
		fn     string
		arg    int
		kinds  []Kind
		callee string
		at     int
	}{
		{fn: "field", arg: 1, kinds: []Kind{StoredIntoField}},
		{fn: "element", arg: 1, kinds: []Kind{StoredIntoElement}},
		{fn: "entry", arg: 1, kinds: []Kind{StoredIntoMap}},
		{fn: "key", arg: 1, kinds: []Kind{StoredIntoMap}},
		{fn: "global1", arg: 0, kinds: []Kind{StoredIntoGlobal}},
		{fn: "global2", arg: 0, kinds: []Kind{StoredIntoGlobal}},
		{fn: "pointer", arg: 1, kinds: []Kind{StoredThroughPointer}},
		{fn: "appended", arg: 1, kinds: []Kind{StoredIntoField, Appended}},
		{fn: "selfStored", arg: 0},
		{fn: "goFunc", arg: 0, kinds: []Kind{PassedToGoroutine}},
		{fn: "copiedInto", arg: 1, kinds: []Kind{StoredIntoElement}},
		{fn: "copiedIntoGlobal", arg: 0, kinds: []Kind{StoredIntoGlobal}},
		{fn: "copied", arg: 1, kinds: []Kind{StoredIntoElement}},
		{fn: "sent", arg: 1, kinds: []Kind{Sent}},
		{fn: "selected", arg: 1, kinds: []Kind{Sent}},
		{fn: "returned", arg: 0, kinds: []Kind{Returned}},
		{fn: "started", arg: 0, kinds: []Kind{PassedToGoroutine}, callee: "reads"},
		{fn: "captured", arg: 0, kinds: []Kind{CapturedByGoroutine}},
		{fn: "keeper", arg: 0, kinds: []Kind{PassedToKeeper}, callee: "keeps"},
		{fn: "consumer", arg: 0, kinds: []Kind{PassedToConsumer}, callee: "takes"},
		{fn: "unknownFunc", arg: 1, kinds: []Kind{PassedToUnknown}},
		{fn: "unknownMethod", arg: 1, kinds: []Kind{PassedToUnknown}, callee: "Put", at: 1},
		{fn: "unknownMethod", arg: 0, kinds: []Kind{PassedToUnknown}, callee: "Put"},
		{fn: "contractedBorrow", arg: 1},
		{fn: "lent", arg: 1, kinds: []Kind{Returned}},
		{fn: "lentOnly", arg: 1},
		{fn: "contractedConsume", arg: 1, kinds: []Kind{PassedToConsumer}, callee: "Contracted", at: 2},
		{fn: "contractedOther", arg: 1, kinds: []Kind{PassedToUnknown}, callee: "Contracted", at: 3},
		{fn: "contractedOther", arg: 0, kinds: []Kind{PassedToUnknown}, callee: "Contracted"},
		{fn: "goMethod", arg: 1, kinds: []Kind{PassedToGoroutine}, callee: "Read", at: 1},
		{fn: "panicked", arg: 0, kinds: []Kind{PassedToPanic}},
		{fn: "fresh", arg: 1, kinds: []Kind{StoredIntoField}},
		{fn: "literalCalled", arg: 1, kinds: []Kind{StoredIntoField}},
		{fn: "literalParam", arg: 1, kinds: []Kind{PassedToKeeper}},
		{fn: "literalStored", arg: 1, kinds: []Kind{StoredIntoField}},
		{fn: "literalPassed", arg: 0, kinds: []Kind{PassedToKeeper}, callee: "run"},
		{fn: "deferred", arg: 0, kinds: []Kind{PassedToKeeper}, callee: "keeps"},
		{fn: "merged", arg: 2, kinds: []Kind{StoredIntoField}},
		{fn: "throughRead", arg: 0},
		{fn: "plain", arg: 0},
	} {
		escapes := Analyze(pkg.Func(tc.fn), fakeLookup{}).Escapes(tc.arg)

		var kinds []Kind
		for _, e := range escapes {
			kinds = append(kinds, e.Kind)
		}

		if !slices.Equal(kinds, tc.kinds) {
			t.Errorf("%s, argument %d: escapes %v, want %v", tc.fn, tc.arg, kinds, tc.kinds)
			continue
		}

		if tc.callee == "" {
			continue
		}

		if e := escapes[0]; e.Callee == nil || e.Callee.Name() != tc.callee || e.Arg != tc.at {
			t.Errorf("%s, argument %d: passed to %v as argument %d, want %s as %d", tc.fn, tc.arg, e.Callee, e.Arg, tc.callee, tc.at)
		}
	}
}

// TestEscapesInsideALiteralPointAtTheLiteral checks that an escape a literal makes of a value
// it captured is reported at the instruction inside the literal, carrying the free variable.
func TestEscapesInsideALiteralPointAtTheLiteral(t *testing.T) {
	fn := buildEscapes(t, escapesSrc).Func("literalDeferred")

	escapes := Analyze(fn, fakeLookup{}).Escapes(1)
	if len(escapes) != 1 {
		t.Fatalf("%d escapes, want 1", len(escapes))
	}

	e := escapes[0]
	if e.Instr.Parent() != fn.AnonFuncs[0] || e.Kind != StoredIntoField {
		t.Errorf("escape at %s in %s, want a store into a field in the literal", e.Instr, e.Instr.Parent())
	}

	// go/ssa captures variables by address: the literal loads the free variable.
	load, isLoad := e.Value.(*ssa.UnOp)
	if _, isFree := load.X.(*ssa.FreeVar); !isLoad || !isFree {
		t.Errorf("escaping value is %s, want a load of the literal's free variable", e.Value)
	}
}

// TestPackedArgumentsEscapeAtTheirStores checks that an argument packed into a variadic call's
// slice is reported as the argument, at the store go/ssa places at it, and that appending a
// value is reported as the value, not the packed slice.
func TestPackedArgumentsEscapeAtTheirStores(t *testing.T) {
	pkg := buildEscapes(t, escapesSrc)

	for _, fn := range []string{"variadic", "appended"} {
		var found bool

		for _, e := range Analyze(pkg.Func(fn), fakeLookup{}).Escapes(len(pkg.Func(fn).Params) - 1) {
			if e.Kind == PassedToKeeper || e.Kind == Appended {
				st, isStore := e.At.(*ssa.Store)
				if _, isParam := e.Value.(*ssa.Parameter); !isStore || !isParam || st.Val != e.Value {
					t.Errorf("%s: escape at %s of %s, want the packing store of the parameter", fn, e.At, e.Value)
				}

				found = true
			}
		}

		if !found {
			t.Errorf("%s: no escape into the callee", fn)
		}
	}
}

// TestOneEscapeThroughSeveralRoots checks that a value carrying two parameters' memory escapes
// once, listed for both.
func TestOneEscapeThroughSeveralRoots(t *testing.T) {
	a := Analyze(buildEscapes(t, escapesSrc).Func("merged"), fakeLookup{})

	if len(a.escapes) != 1 || len(a.Escapes(2)) != 1 || len(a.Escapes(3)) != 1 || a.Escapes(1) != nil {
		t.Errorf("%d escapes; %d for a, %d for b, %d for the holder; want 1, 1, 1, 0",
			len(a.escapes), len(a.Escapes(2)), len(a.Escapes(3)), len(a.Escapes(1)))
	}
}
