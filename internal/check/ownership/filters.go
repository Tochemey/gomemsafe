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
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/ssa"
)

// rangeLoopBlock is the comment go/ssa gives the header of a range loop over a slice: the
// block that increments the index and tests it against the length.
const rangeLoopBlock = "rangeindex.loop"

// varargsComment is the comment go/ssa gives the array it packs variadic arguments into.
const varargsComment = "varargs"

// filter is an in-place filter: a range loop over a slice that appends the elements it keeps
// to a prefix of that slice, as in
//
//	out := a[:0]
//	for _, x := range a {
//		if keep(x) {
//			out = append(out, x)
//		}
//	}
//
// Each iteration appends at most one element, so an append writes at an index no greater than
// the loop's, and the loop reads each element before any append can reach it. Its read of the
// ranged slice at the loop index is intact when no append but the filter's own ran since the
// slice was made or the loop entered (fact unchanged), and none ran yet in this iteration
// (fact fresh), mirroring the Go wiki's SliceTricks, "Filtering without allocating".
type filter struct {
	class            int
	unchanged, fresh int
	header           *ssa.BasicBlock
	loop             map[*ssa.BasicBlock]bool
}

// addFilters finds the range loops that read a followed slice at the loop index or ahead of
// it, and records which overwriting appends belong to each loop's filter.
func (a *aliasing) addFilters() {
	for _, header := range a.fn.Blocks {
		if header.Comment != rangeLoopBlock {
			continue
		}

		index := loopIndex(header)
		loop := loopBlocks(header)
		byClass := make(map[int]int)

		for _, read := range aheadReads(index) {
			c, tracked := a.classOf[read.X]
			if !tracked {
				continue
			}

			f, seen := byClass[c]
			if !seen {
				f = a.addFilter(c, header, index, loop)
				byClass[c] = f
			}

			a.reads[read] = f
		}
	}

	for i := range a.overwrites {
		ow := &a.overwrites[i]

		for _, c := range ow.classes {
			for _, f := range a.filtersOf[c] {
				if a.belongs(ow.call, a.filters[f]) {
					ow.own = append(ow.own, f)
				} else {
					ow.other = append(ow.other, f)
				}
			}
		}
	}
}

// addFilter adds the filter of class c's range loop at header, with its two facts, and returns
// its index.
func (a *aliasing) addFilter(c int, header *ssa.BasicBlock, index ssa.Value, loop map[*ssa.BasicBlock]bool) int {
	f := len(a.filters)
	a.filters = append(a.filters, filter{
		class: c, unchanged: a.n, fresh: a.n + 1, header: header, loop: loop,
	})
	a.n += 2
	a.filtersOf[c] = append(a.filtersOf[c], f)
	a.counters[index] = append(a.counters[index], f)

	for _, pred := range header.Preds {
		if !loop[pred] {
			a.entries[edge{pred, header}] = append(a.entries[edge{pred, header}], f)
		}
	}

	return f
}

// belongs reports whether call is one of f's own appends: inside the loop, it appends one
// element to the slice the loop carries, which starts as an empty prefix of the ranged slice
// and grows only through such appends.
func (a *aliasing) belongs(call *ssa.Call, f filter) bool {
	out, ok := call.Call.Args[0].(*ssa.Phi)
	if !ok || out.Block() != f.header || !appendsOne(call) {
		return false
	}

	for i, v := range out.Edges {
		if f.loop[f.header.Preds[i]] {
			if !growsBy(v, out, f.loop) {
				return false
			}
		} else if !a.isEmptyPrefix(v, f.class) {
			return false
		}
	}

	return true
}

// isEmptyPrefix reports whether v is x[:0] or x[0:0] of the slice of class c.
func (a *aliasing) isEmptyPrefix(v ssa.Value, c int) bool {
	s, ok := v.(*ssa.Slice)
	if !ok || s.High == nil || !isZero(s.High) || s.Low != nil && !isZero(s.Low) {
		return false
	}

	xc, tracked := a.classOf[s.X]

	return tracked && xc == c
}

// growsBy reports whether v, the value a loop carries back to its phi out, is out itself, or
// out with one element appended, through the phis inside the loop.
func growsBy(v ssa.Value, out *ssa.Phi, loop map[*ssa.BasicBlock]bool) bool {
	seen := make(map[ssa.Value]bool)
	work := []ssa.Value{v}

	for len(work) > 0 {
		v := work[len(work)-1]
		work = work[:len(work)-1]

		if v == out || seen[v] {
			continue
		}

		seen[v] = true

		switch x := v.(type) {
		case *ssa.Phi:
			if !loop[x.Block()] {
				return false
			}

			work = append(work, x.Edges...)
		case *ssa.Call:
			if !isBuiltin(x.Call.Value, appendBuiltin) || x.Call.Args[0] != out || !appendsOne(x) {
				return false
			}
		default:
			return false
		}
	}

	return true
}

// appendsOne reports whether call appends exactly one element: go/ssa packs the elements an
// append lists into a varargs array, and passes nil when it lists none.
func appendsOne(call *ssa.Call) bool {
	s, ok := call.Call.Args[1].(*ssa.Slice)
	if !ok {
		return false
	}

	alloc, ok := s.X.(*ssa.Alloc)
	if !ok || alloc.Comment != varargsComment {
		return false
	}

	arr, ok := alloc.Type().(*types.Pointer).Elem().Underlying().(*types.Array)

	return ok && arr.Len() == 1
}

// aheadReads returns the instructions that index into a slice at index, or at index plus a
// constant that is not negative, as a dedup loop reads a[i+1]: an in-place filter's appends
// write behind them.
func aheadReads(index ssa.Value) []*ssa.IndexAddr {
	var out []*ssa.IndexAddr

	for _, r := range referrersOf(index) {
		switch x := r.(type) {
		case *ssa.IndexAddr:
			if x.Index == index {
				out = append(out, x)
			}
		case *ssa.BinOp:
			if x.Op == token.ADD && (isNonNegative(x.X) || isNonNegative(x.Y)) {
				out = append(out, aheadReads(x)...)
			}
		}
	}

	return out
}

// loopIndex returns the index a range loop's header computes: the increment that its closing
// branch tests against the length.
func loopIndex(header *ssa.BasicBlock) ssa.Value {
	return header.Instrs[len(header.Instrs)-1].(*ssa.If).Cond.(*ssa.BinOp).X
}

// loopBlocks returns the blocks of header's natural loop: header, and the blocks it dominates
// that reach one of its back edges.
func loopBlocks(header *ssa.BasicBlock) map[*ssa.BasicBlock]bool {
	loop := map[*ssa.BasicBlock]bool{header: true}

	var work []*ssa.BasicBlock

	for _, pred := range header.Preds {
		if header.Dominates(pred) && !loop[pred] {
			loop[pred] = true
			work = append(work, pred)
		}
	}

	for len(work) > 0 {
		b := work[len(work)-1]
		work = work[:len(work)-1]

		for _, pred := range b.Preds {
			if !loop[pred] && header.Dominates(pred) {
				loop[pred] = true
				work = append(work, pred)
			}
		}
	}

	return loop
}

// isNonNegative reports whether v is a constant that is not negative.
func isNonNegative(v ssa.Value) bool {
	c, ok := v.(*ssa.Const)
	return ok && c.Value != nil && constant.Sign(c.Value) >= 0
}

// isZero reports whether v is the constant 0.
func isZero(v ssa.Value) bool {
	c, ok := v.(*ssa.Const)
	return ok && c.Value != nil && c.Int64() == 0
}
