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

package invalidation

import "golang.org/x/tools/go/ssa"

// Base strips conversions and reslices that keep the same memory, returning the value they
// started from.
func Base(v ssa.Value) ssa.Value {
	for {
		switch x := v.(type) {
		case *ssa.MakeInterface:
			v = x.X
		case *ssa.Slice:
			v = x.X
		case *ssa.ChangeType:
			v = x.X
		case *ssa.ChangeInterface:
			v = x.X
		case *ssa.TypeAssert:
			if x.CommaOk {
				return v
			}

			v = x.X
		case *ssa.Extract:
			ta, ok := x.Tuple.(*ssa.TypeAssert)
			if !ok || x.Index != 0 {
				return v
			}

			v = ta.X
		default:
			return v
		}
	}
}

// class returns the class whose base is v, creating it if needed.
func (a *Analysis) class(v ssa.Value) int {
	if c, ok := a.classOf[v]; ok {
		return c
	}

	c := len(a.bases)
	a.bases = append(a.bases, v)
	a.classOf[v] = c

	return c
}

// closeOverAliases adds to each class every value derived from its members by a
// pointer-preserving conversion, and gives each phi fed by a tracked value its own class.
func (a *Analysis) closeOverAliases() {
	work := append([]ssa.Value(nil), a.bases...)
	for len(work) > 0 {
		v := work[len(work)-1]
		work = work[:len(work)-1]
		c := a.classOf[v]

		for _, r := range referrersOf(v) {
			derived, ok := r.(ssa.Value)
			if !ok {
				continue
			}

			if _, seen := a.classOf[derived]; seen {
				continue
			}

			work = a.addDerived(derived, c, work)
		}
	}
}

// addDerived puts derived, a referrer of a member of class c, into the right class and returns
// the work list extended with the values whose referrers must be visited next.
func (a *Analysis) addDerived(derived ssa.Value, c int, work []ssa.Value) []ssa.Value {
	switch d := derived.(type) {
	case *ssa.Phi:
		a.class(d)
		return append(work, d)
	case *ssa.TypeAssert:
		a.classOf[d] = c
		if !d.CommaOk {
			return append(work, d)
		}

		for _, r := range referrersOf(d) {
			if ex, ok := r.(*ssa.Extract); ok && ex.Index == 0 {
				a.classOf[ex] = c
				work = append(work, ex)
			}
		}

		return work
	case *ssa.MakeInterface, *ssa.ChangeType, *ssa.ChangeInterface, *ssa.Slice:
		a.classOf[d] = c
		return append(work, d)
	}

	return work
}

// sourceClasses returns c followed by the classes that flow into it through phis, nearest
// first.
func (a *Analysis) sourceClasses(c int) []int {
	order := []int{c}
	seen := map[int]bool{c: true}

	for i := 0; i < len(order); i++ {
		phi, ok := a.bases[order[i]].(*ssa.Phi)
		if !ok {
			continue
		}

		for _, v := range phi.Edges {
			if ec, tracked := a.classOf[v]; tracked && !seen[ec] {
				seen[ec] = true
				order = append(order, ec)
			}
		}
	}

	return order
}

func referrersOf(v ssa.Value) []ssa.Instruction {
	if refs := v.Referrers(); refs != nil {
		return *refs
	}

	return nil
}
