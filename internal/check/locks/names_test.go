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

package locks

import (
	"go/constant"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"
)

func TestElementTypesAreFoundThroughPointers(t *testing.T) {
	elem := types.Typ[types.Int]

	for _, tc := range []struct {
		t, want types.Type
	}{
		{types.NewSlice(elem), elem},
		{types.NewPointer(types.NewPointer(types.NewArray(elem, 2))), elem},
		{types.Typ[types.String], types.Typ[types.Invalid]},
	} {
		if got := elementOf(tc.t); got != tc.want {
			t.Errorf("elementOf(%s) = %s, want %s", tc.t, got, tc.want)
		}
	}
}

// TestNamesEndAtAStepTheTypeCannotFollow names keys whose path leads where the type has no
// such field or element, which no access builds: the name keeps the steps followed so far.
func TestNamesEndAtAStepTheTypeCannotFollow(t *testing.T) {
	pkg := buildSSA(t, identitySrc)
	param := pkg.Func("values").Params[0]
	u := &universe{indexKeys: []key{constKey(ssa.NewConst(constant.MakeInt64(1), types.Typ[types.Int]))}}
	c := &checker{pass: &analysis.Pass{Pkg: pkg.Pkg}}

	for k, want := range map[key]string{
		key{root: param}.field(1):          "t.n",
		key{root: param}.field(5):          "t",
		key{root: param}.field(1).field(0): "t.n",
		key{root: param}.index(0).field(0): "t[1]",
	} {
		if got := c.name(u, k); got != want {
			t.Errorf("name(%v) = %q, want %q", k.path, got, want)
		}
	}
}

// TestGlobalNamesQualifyByPackage names a global of the package itself bare, and one of a
// package reached through facts alone, which is not imported, by the last element of its path.
func TestGlobalNamesQualifyByPackage(t *testing.T) {
	pkg := buildSSA(t, identitySrc)
	c := &checker{pass: &analysis.Pass{Pkg: pkg.Pkg}}

	if name, typ := c.globalName("p.use"); name != "use" || typ == types.Typ[types.Invalid] {
		t.Errorf("globalName(p.use) = %q, %s", name, typ)
	}

	if name, typ := c.globalName("example.com/far.mu"); name != "far.mu" || typ != types.Typ[types.Invalid] {
		t.Errorf("globalName(example.com/far.mu) = %q, %s", name, typ)
	}
}
