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

package ownership

import (
	"testing"

	"golang.org/x/tools/go/ssa"
)

const filtersSrc = `package p

func filter(a []int) []int {
	out := a[:0]
	for _, x := range a {
		if x > 0 {
			out = append(out, x)
		}
	}
	return out
}

func doubled(a []int) []int {
	out := a[:0]
	for _, x := range a {
		out = append(out, x, x)
	}
	return out
}

func ahead(a []int) []int {
	out := a[:1]
	for _, x := range a {
		out = append(out, x)
	}
	return out
}

func indexes(a []int) []int {
	out := a[:0]
	for i := range a {
		out = append(out, a[i])
	}
	return out
}
`

// TestRecognisesInPlaceFilters checks which appends belong to a range loop's in-place filter:
// one element at a time, to a prefix that starts empty.
func TestRecognisesInPlaceFilters(t *testing.T) {
	pkg := buildSource(t, filtersSrc)

	for name, own := range map[string]bool{"filter": true, "indexes": true, "doubled": false, "ahead": false} {
		a := newAliasing(pkg.Func(name), func(ssa.Value) bool { return true })
		if len(a.filters) != 1 || len(a.reads) == 0 {
			t.Fatalf("%s: %d filters and %d reads, want one filter and its reads", name, len(a.filters), len(a.reads))
		}

		for _, ow := range a.overwrites {
			if got := len(ow.own) == 1; got != own || len(ow.own)+len(ow.other) != 1 {
				t.Errorf("%s: %s belongs to %d filters and not to %d, want belonging %v", name, ow.call, len(ow.own), len(ow.other), own)
			}
		}
	}
}

// TestFindsTheLoopOfAFilter checks that a filter's loop holds its body and not what follows.
func TestFindsTheLoopOfAFilter(t *testing.T) {
	a := newAliasing(buildSource(t, filtersSrc).Func("filter"), func(ssa.Value) bool { return true })
	f := a.filters[0]

	for _, b := range a.fn.Blocks {
		inside := f.loop[b]
		if want := b == f.header || b.Comment == "rangeindex.body" || b.Comment == "if.then"; inside != want {
			t.Errorf("block %d (%s) in the loop: %v, want %v", b.Index, b.Comment, inside, want)
		}
	}
}
