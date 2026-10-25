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

package unsafeflow

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/flow"
	"github.com/tochemey/gomemsafe/internal/lazyssa"
	"github.com/tochemey/gomemsafe/internal/locate"
	"github.com/tochemey/gomemsafe/internal/prefilter"
	"github.com/tochemey/gomemsafe/internal/report"
	"github.com/tochemey/gomemsafe/internal/trace"
)

// Diagnostic texts of MS102. The string's source text is the first %s after the ID.
const (
	msgMutatedView    = "%s: %s used after a write to the bytes it shares"
	msgSharesBuffer   = "%s shares the bytes of %s through unsafe.String"
	msgSharesString   = "%s shares its bytes with a []byte through unsafe.Slice"
	msgBytesWritten   = "the bytes of %s are written here"
	helpCopyBuffer    = "copy the bytes with string(%s) instead of unsafe.String, or write %s only after the last use of %s"
	helpCopyString    = "write into a copy, []byte(%s), not into the bytes unsafe.Slice shares with %s"
	unknownOwnerText  = "the buffer"
	unknownStringText = "string"
)

// Names of the universe builtins MS102 follows, beside package unsafe's (file safety.go).
const (
	lenBuiltin   = "len"
	copyBuiltin  = "copy"
	clearBuiltin = "clear"
)

// stringView is a string that shares its bytes with a byte buffer through package unsafe: s in
// s := unsafe.String(&b[i], n), with b the buffer, or s in v := unsafe.Slice(unsafe.StringData(s),
// n), with v the buffer. A write into the buffer that lands in the string's bytes changes the
// string, which Go promises never happens.
type stringView struct {
	call *ssa.Call
	// class is the string's class: the string and the values that are it under another type
	// or length.
	class int
	// buffer is the byte slice, or the pointer to a byte array, that the bytes are written
	// through.
	buffer ssa.Value
	// start is the index in buffer of the string's first byte, and length its number of bytes,
	// or -1 when it is not a constant. whole is set when the string covers every byte of
	// buffer, so that any write into buffer lands in it.
	start, length int64
	whole         bool
	fromString    bool
}

// mutations is the problem MS102 solves for one function: a string's class is set after a
// write into its bytes ran on every path, and cleared where the string is made again.
type mutations struct {
	views   []stringView
	bases   []ssa.Value
	classOf map[ssa.Value]int
	// writes maps each instruction that writes into the bytes of some views to them.
	writes map[ssa.Instruction][]int
}

// mutationChecker reports MS102 in the functions of one package.
type mutationChecker struct {
	pass  *analysis.Pass
	loc   *locate.Locator
	calls map[token.Pos]*ast.CallExpr
	eng   flow.Engine
}

// write is a store, copy, or clear into a buffer: through buffer itself at constant index, or
// at an unknown one, or through a reslice of it that keeps its length to the end.
type write struct {
	instr  ssa.Instruction
	buffer ssa.Value
	index  int64
	direct bool
	// all is set when the write may write any of the buffer's bytes: copy, clear, or the
	// store of a whole array.
	all bool
}

// N implements flow.Problem.
func (m *mutations) N() int { return len(m.bases) }

// Phi implements flow.Problem: a phi starts no class, so it changes nothing.
func (*mutations) Phi(*ssa.Phi, func(int) *flow.Bitset, *flow.Bitset) {}

// Transfer implements flow.Problem: a write marks the strings it lands in, and making a string
// again, as in a loop, clears its class.
func (m *mutations) Transfer(instr ssa.Instruction, state *flow.Bitset) {
	for _, v := range m.writes[instr] {
		state.Set(m.views[v].class)
	}

	if v, ok := instr.(ssa.Value); ok {
		if c, tracked := m.classOf[v]; tracked && m.bases[c] == v {
			state.Clear(c)
		}
	}
}

// reportMutatedViews reports MS102 in every function declaration of the package that makes a
// string view: views lists the positions of its unsafe.String and unsafe.Slice calls. Only
// those functions are lowered to SSA.
func reportMutatedViews(pass *analysis.Pass, views []token.Pos) {
	if len(views) == 0 {
		return
	}

	pre := pass.ResultOf[prefilter.Analyzer].(*prefilter.Result)
	ssaRes := pass.ResultOf[lazyssa.Analyzer].(*lazyssa.Result)
	c := &mutationChecker{pass: pass, loc: locate.New(pass, pre.Calls), calls: pre.Calls}

	next := 0

	for _, f := range pre.Funcs {
		for next < len(views) && views[next] < f.Decl.Pos() {
			next++
		}

		if next == len(views) || views[next] >= f.Decl.End() {
			continue
		}

		if fn := ssaRes.Func(f.Obj); fn != nil {
			c.function(fn)
		}
	}

	for range c.eng.BudgetHits {
		trace.Record(pass.Pkg.Path(), pass.Analyzer.Name, trace.KindBudget, 0)
	}
}

// makesStringView reports whether call may make a string view that MS102 follows: any
// unsafe.String, and unsafe.Slice of the bytes of a string that is not a constant. Generated
// protobuf code makes a []byte of a constant descriptor that way in every package, and a
// constant's bytes are never written.
func makesStringView(info *types.Info, call *ast.CallExpr) bool {
	name, ok := unsafeBuiltin(info, call.Fun)

	switch {
	case !ok:
		return false
	case name == stringBuiltin:
		return true
	case name != sliceBuiltin || len(call.Args) != 2:
		return false
	}

	data, ok := ast.Unparen(call.Args[0]).(*ast.CallExpr)
	if !ok || len(data.Args) != 1 {
		return false
	}

	name, ok = unsafeBuiltin(info, data.Fun)

	return ok && name == stringDataBuiltin && info.Types[data.Args[0]].Value == nil
}

// newMutations finds fn's string views and the writes that land in their bytes.
func newMutations(fn *ssa.Function) *mutations {
	m := &mutations{classOf: make(map[ssa.Value]int), writes: make(map[ssa.Instruction][]int)}

	var writes []write

	for _, b := range fn.Blocks {
		for _, instr := range b.Instrs {
			if call, ok := instr.(*ssa.Call); ok {
				if view, ok := m.viewOf(call); ok {
					m.views = append(m.views, view)
				}
			}

			if w, ok := writeOf(instr); ok {
				writes = append(writes, w)
			}
		}
	}

	if len(m.views) == 0 {
		return m
	}

	m.closeOverAliases()

	for _, w := range writes {
		for i, v := range m.views {
			if v.buffer == w.buffer && v.hitBy(w) {
				m.writes[w.instr] = append(m.writes[w.instr], i)
			}
		}
	}

	return m
}

// viewOf returns the string view call makes, if it is unsafe.String of a byte buffer's address
// or unsafe.Slice of a string's bytes.
func (m *mutations) viewOf(call *ssa.Call) (stringView, bool) {
	args := call.Common().Args

	switch builtinName(call) {
	case stringBuiltin:
		buffer, start, ok := bufferAddress(args[0])
		if !ok {
			return stringView{}, false
		}

		view := stringView{call: call, class: m.class(call), buffer: buffer, start: start, length: constInt(args[1])}
		view.whole = start == 0 && (isLenOf(args[1], buffer) || view.length >= 0 && view.length == arrayLen(buffer))

		return view, true
	case sliceBuiltin:
		data, ok := args[0].(*ssa.Call)
		if !ok || builtinName(data) != stringDataBuiltin {
			return stringView{}, false
		}

		s := data.Common().Args[0]
		if _, isConst := s.(*ssa.Const); isConst {
			return stringView{}, false
		}

		// The slice lies within the string, so every write into it lands in the string.
		return stringView{call: call, class: m.class(s), buffer: call, length: -1, whole: true, fromString: true}, true
	}

	return stringView{}, false
}

// hitBy reports whether w lands in the view's bytes. A string covering the whole buffer is hit
// by any write into it. Otherwise only a write through the buffer itself counts: at a constant
// index inside the string, or, when the string's length is not a constant, at its first byte,
// which a string that is not empty always has. A clear of the buffer zeroes the string's first
// byte, and a copy into it writes that byte when the string starts the buffer.
func (v stringView) hitBy(w write) bool {
	switch {
	case v.whole:
		return true
	case !w.direct || v.length == 0:
		return false
	case w.all:
		return builtinName(w.instr) != copyBuiltin || v.start == 0
	case w.index < v.start:
		return false
	case v.length < 0:
		return w.index == v.start
	}

	return w.index < v.start+v.length
}

// function reports the strings used after a write into their bytes in fn and in every function
// literal nested in it.
func (c *mutationChecker) function(fn *ssa.Function) {
	m := newMutations(fn)
	if len(m.writes) > 0 {
		if sol, outcome := c.eng.Solve(fn, m); outcome == flow.Solved {
			reported := make(map[int]bool)

			sol.Walk(func(instr ssa.Instruction, state *flow.Bitset) {
				c.reportUses(m, instr, state, reported)
			})
		}
	}

	for _, anon := range fn.AnonFuncs {
		c.function(anon)
	}
}

// reportUses reports the first use, among instr's operands, of each string of m whose bytes
// were written on every path to instr.
func (c *mutationChecker) reportUses(m *mutations, instr ssa.Instruction, state *flow.Bitset, reported map[int]bool) {
	if !readsBytes(instr) {
		return
	}

	var ops [8]*ssa.Value

	for _, op := range instr.Operands(ops[:0]) {
		if op == nil || *op == nil {
			continue
		}

		class, tracked := m.classOf[*op]
		if !tracked || reported[class] || !state.Has(class) {
			continue
		}

		reported[class] = true

		view, written := m.lastWrite(class, instr.Pos())
		name := valueName(c.loc.Operand(instr, *op))
		d := analysis.Diagnostic{
			Pos:      c.loc.UsePos(instr, *op, view.call.Pos()),
			Category: MutationID.String(),
			Message:  fmt.Sprintf(msgMutatedView, MutationID, name),
			Related: []analysis.RelatedInformation{
				{Pos: view.call.Pos()},
				{Pos: written.Pos(), Message: fmt.Sprintf(msgBytesWritten, name)},
			},
		}

		if view.fromString {
			d.Related[0].Message = fmt.Sprintf(msgSharesString, name)
			d.SuggestedFixes = []analysis.SuggestedFix{{Message: fmt.Sprintf(helpCopyString, name, name)}}
		} else {
			buffer := bufferText(c.calls, view.call)
			d.Related[0].Message = fmt.Sprintf(msgSharesBuffer, name, buffer)
			d.SuggestedFixes = []analysis.SuggestedFix{{Message: fmt.Sprintf(helpCopyBuffer, buffer, buffer, name)}}
		}

		report.Report(c.pass, d)
	}
}

// lastWrite returns a view of class c and the write into its bytes closest before pos, or the
// first such write when none precedes pos, such as one later in a loop.
func (m *mutations) lastWrite(c int, pos token.Pos) (stringView, ssa.Instruction) {
	var best, first ssa.Instruction

	view := -1

	for instr, views := range m.writes {
		for _, v := range views {
			if m.views[v].class != c {
				continue
			}

			if first == nil || instr.Pos() < first.Pos() {
				first = instr
			}

			if instr.Pos() < pos && (best == nil || instr.Pos() > best.Pos()) {
				best, view = instr, v
			}
		}
	}

	if best == nil {
		best = first

		for _, v := range m.writes[first] {
			if m.views[v].class == c {
				view = v
			}
		}
	}

	return m.views[view], best
}

// class returns the class whose base is v, creating it if needed.
func (m *mutations) class(v ssa.Value) int {
	if c, ok := m.classOf[v]; ok {
		return c
	}

	c := len(m.bases)
	m.bases = append(m.bases, v)
	m.classOf[v] = c

	return c
}

// closeOverAliases adds to each string's class the values that are the same bytes under another
// type or length: a reslice, a conversion to a named string type or an interface, and an
// assertion back. It mirrors package invalidation's alias classes without their phis: a phi is
// not followed, so a string kept across loop iterations is missed.
func (m *mutations) closeOverAliases() {
	work := append([]ssa.Value(nil), m.bases...)
	for len(work) > 0 {
		v := work[len(work)-1]
		work = work[:len(work)-1]

		refs := v.Referrers()
		if refs == nil {
			continue
		}

		for _, r := range *refs {
			switch d := r.(type) {
			case *ssa.Slice, *ssa.ChangeType, *ssa.MakeInterface, *ssa.ChangeInterface:
				derived := d.(ssa.Value)
				if _, seen := m.classOf[derived]; !seen {
					m.classOf[derived] = m.classOf[v]
					work = append(work, derived)
				}
			case *ssa.TypeAssert:
				if _, seen := m.classOf[d]; !seen && !d.CommaOk {
					m.classOf[d] = m.classOf[v]
					work = append(work, d)
				}
			}
		}
	}
}

// writeOf returns the write instr makes into a byte buffer, if it makes one: a store through an
// element's address or into a whole array, a copy, or a clear.
func writeOf(instr ssa.Instruction) (write, bool) {
	switch x := instr.(type) {
	case *ssa.Store:
		if elem, ok := x.Addr.(*ssa.IndexAddr); ok {
			buffer, direct := reslicedFrom(elem.X)
			return write{instr: instr, buffer: buffer, index: constInt(elem.Index), direct: direct}, true
		}

		if arrayLen(x.Addr) >= 0 {
			return write{instr: instr, buffer: x.Addr, index: -1, direct: true, all: true}, true
		}
	case *ssa.Call:
		switch builtinName(x) {
		case copyBuiltin, clearBuiltin:
			dst := x.Common().Args[0]
			if _, isSlice := dst.Type().Underlying().(*types.Slice); !isSlice {
				return write{}, false
			}

			buffer, direct := reslicedFrom(dst)

			return write{instr: instr, buffer: buffer, index: -1, direct: direct, all: true}, true
		}
	}

	return write{}, false
}

// bufferAddress returns the buffer that p, the first argument of unsafe.String, points into,
// and the index it points at: b and i for &b[i] with a constant i, b and 0 for
// unsafe.SliceData(b).
func bufferAddress(p ssa.Value) (ssa.Value, int64, bool) {
	switch x := p.(type) {
	case *ssa.IndexAddr:
		start := constInt(x.Index)
		return x.X, start, start >= 0
	case *ssa.Call:
		if builtinName(x) == sliceDataBuiltin {
			return x.Common().Args[0], 0, true
		}
	}

	return nil, 0, false
}

// reslicedFrom returns the buffer v is a reslice of, through reslices that keep the length to
// the end, such as b[k:], so that every index of v is an index of the buffer; direct is set
// when v is the buffer itself. A conversion to another byte slice type keeps the indexes too.
func reslicedFrom(v ssa.Value) (ssa.Value, bool) {
	direct := true

	for {
		switch x := v.(type) {
		case *ssa.Slice:
			if x.High != nil || x.Max != nil {
				return v, direct
			}

			v, direct = x.X, false
		case *ssa.ChangeType:
			v = x.X
		default:
			return v, direct
		}
	}
}

// readsBytes reports whether instr reads the bytes of its string operands: not len, which
// reads the length only, nor unsafe.StringData, which reads the address, nor a conversion or a
// reslice, which only makes another value of the same bytes, nor a comparison with "", which
// only compares lengths.
func readsBytes(instr ssa.Instruction) bool {
	switch x := instr.(type) {
	case *ssa.Slice, *ssa.ChangeType, *ssa.MakeInterface, *ssa.ChangeInterface, *ssa.TypeAssert,
		*ssa.Extract, *ssa.Phi, *ssa.DebugRef:
		return false
	case *ssa.Call:
		name := builtinName(x)
		return name != lenBuiltin && name != stringDataBuiltin
	case *ssa.BinOp:
		return !isEmptyString(x.X) && !isEmptyString(x.Y)
	}

	return true
}

// builtinName returns the name of the builtin instr calls, or "" when it calls none.
func builtinName(instr ssa.Instruction) string {
	call, ok := instr.(*ssa.Call)
	if !ok {
		return ""
	}

	if b, ok := call.Common().Value.(*ssa.Builtin); ok {
		return b.Name()
	}

	return ""
}

// isLenOf reports whether n is len(buffer).
func isLenOf(n, buffer ssa.Value) bool {
	call, ok := n.(*ssa.Call)
	return ok && builtinName(call) == lenBuiltin && call.Common().Args[0] == buffer
}

// constInt returns v as a constant integer, or -1 when it is not a non-negative constant.
func constInt(v ssa.Value) int64 {
	c, ok := v.(*ssa.Const)
	if !ok || c.Value == nil || c.Value.Kind() != constant.Int {
		return -1
	}

	n, exact := constant.Int64Val(c.Value)
	if !exact || n < 0 {
		return -1
	}

	return n
}

// arrayLen returns the length of the array v points to, or -1 when v is no pointer to an array.
// A view's buffer is always a byte array or slice, because unsafe.String takes a *byte.
func arrayLen(v ssa.Value) int64 {
	ptr, ok := v.Type().Underlying().(*types.Pointer)
	if !ok {
		return -1
	}

	array, ok := ptr.Elem().Underlying().(*types.Array)
	if !ok {
		return -1
	}

	return array.Len()
}

func isEmptyString(v ssa.Value) bool {
	c, ok := v.(*ssa.Const)
	return ok && c.Value != nil && c.Value.Kind() == constant.String && constant.StringVal(c.Value) == ""
}

// bufferText returns the source text of the buffer an unsafe.String call makes its string
// from: b in unsafe.String(&b[0], n) or unsafe.String(unsafe.SliceData(b), n).
func bufferText(calls map[token.Pos]*ast.CallExpr, view *ssa.Call) string {
	call, ok := calls[view.Pos()]
	if !ok || len(call.Args) == 0 {
		return unknownOwnerText
	}

	switch p := ast.Unparen(call.Args[0]).(type) {
	case *ast.UnaryExpr:
		if index, ok := ast.Unparen(p.X).(*ast.IndexExpr); ok {
			return types.ExprString(index.X)
		}
	case *ast.CallExpr:
		if len(p.Args) == 1 {
			return types.ExprString(p.Args[0])
		}
	}

	return unknownOwnerText
}

// valueName names the string an operand expression carries, or the string in general when the
// expression is not found.
func valueName(expr ast.Expr) string {
	if expr == nil {
		return unknownStringText
	}

	return types.ExprString(ast.Unparen(expr))
}
