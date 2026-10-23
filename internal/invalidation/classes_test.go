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

package invalidation_test

import (
	"testing"

	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/invalidation"
)

func TestBaseStripsConversionsThatKeepTheMemory(t *testing.T) {
	pkg := build(t)

	for _, name := range []string{"viaInterface", "resliced"} {
		fn := pkg.Func(name)
		param := fn.Params[0]

		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				v, ok := instr.(ssa.Value)
				if !ok {
					continue
				}

				switch v.(type) {
				case *ssa.MakeInterface, *ssa.TypeAssert, *ssa.Slice:
					if got := invalidation.Base(v); got != param {
						t.Errorf("%s: Base(%s) = %s, want the parameter", name, v.Name(), got.Name())
					}
				}
			}
		}
	}
}

// baseSrc has a value of each kind that Base either strips or keeps.
const baseSrc = `package p

import "io"

func pair() (*int, error) { return nil, nil }

func changeInterface(rw io.ReadWriter) io.Reader { return rw }

func commaOk(i any) (*int, bool) { x, ok := i.(*int); return x, ok }

func callResult() *int { x, _ := pair(); return x }
`

// TestBaseStripsOnlyTheSameMemory covers each kind of value Base looks at: an interface
// conversion and the value of a comma-ok assertion are the same memory as their operand, while
// the comma-ok assertion itself, its ok result, and a call's result are values of their own.
func TestBaseStripsOnlyTheSameMemory(t *testing.T) {
	pkg := buildSource(t, baseSrc)

	for _, tc := range []struct {
		fn   string
		want func(v ssa.Value, fn *ssa.Function) ssa.Value
		kind func(ssa.Value) bool
	}{
		{"changeInterface", param, isChangeInterface},
		{"commaOk", param, isFirstOfAssertion},
		{"commaOk", itself, isCommaOkAssertion},
		{"commaOk", itself, isSecondOfAssertion},
		{"callResult", itself, isExtractOfCall},
	} {
		fn := pkg.Func(tc.fn)
		found := false

		for _, b := range fn.Blocks {
			for _, instr := range b.Instrs {
				v, ok := instr.(ssa.Value)
				if !ok || !tc.kind(v) {
					continue
				}

				found = true

				if got, want := invalidation.Base(v), tc.want(v, fn); got != want {
					t.Errorf("%s: Base(%s) = %s, want %s", tc.fn, v, got, want)
				}
			}
		}

		if !found {
			t.Errorf("%s: no value of the kind under test", tc.fn)
		}
	}
}

func param(_ ssa.Value, fn *ssa.Function) ssa.Value { return fn.Params[0] }

func itself(v ssa.Value, _ *ssa.Function) ssa.Value { return v }

func isChangeInterface(v ssa.Value) bool {
	_, ok := v.(*ssa.ChangeInterface)
	return ok
}

func isCommaOkAssertion(v ssa.Value) bool {
	ta, ok := v.(*ssa.TypeAssert)
	return ok && ta.CommaOk
}

func isFirstOfAssertion(v ssa.Value) bool { return isExtractOf(v, 0, isCommaOkAssertion) }

func isSecondOfAssertion(v ssa.Value) bool { return isExtractOf(v, 1, isCommaOkAssertion) }

func isExtractOfCall(v ssa.Value) bool {
	return isExtractOf(v, 0, func(t ssa.Value) bool {
		_, ok := t.(*ssa.Call)
		return ok
	})
}

func isExtractOf(v ssa.Value, index int, tuple func(ssa.Value) bool) bool {
	ex, ok := v.(*ssa.Extract)
	return ok && ex.Index == index && tuple(ex.Tuple)
}
