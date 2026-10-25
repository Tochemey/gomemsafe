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

	"github.com/tochemey/gomemsafe/internal/flow"
	"github.com/tochemey/gomemsafe/internal/invalidation"
)

// viewSrc uses the standard library views that package effect knows.
const viewSrc = `package p

import (
	"bufio"
	"bytes"
)

func use(b []byte) {}

func release(x any) {}

type conn struct{ buf bytes.Buffer }

type embeds struct{ bytes.Buffer }

type twoBuffers struct{ a, b bytes.Buffer }

type pointerField struct{ buf *bytes.Buffer }

func field(c *conn) { b := c.buf.Bytes(); c.buf.Reset(); use(b) }

func embedded(e *embeds) { b := e.Bytes(); e.Reset(); use(b) }

func otherConn(x, y *conn) { b := x.buf.Bytes(); y.buf.Reset(); use(b) }

func otherField(t *twoBuffers) { b := t.a.Bytes(); t.b.Reset(); use(b) }

func connEachIteration(conns []*conn) {
	var prev []byte
	for _, c := range conns {
		c.buf.WriteByte('x')
		use(prev)
		prev = c.buf.Bytes()
	}
}

func behindPointer(p *pointerField) { b := p.buf.Bytes(); p.buf.Reset(); use(b) }

func captured(buf *bytes.Buffer) func() {
	return func() {
		b := buf.Bytes()
		buf.Reset()
		use(b)
	}
}

func reset(buf *bytes.Buffer) { b := buf.Bytes(); buf.Reset(); use(b) }

func scan(sc *bufio.Scanner) { x := sc.Bytes(); sc.Scan(); use(x) }

func peek(r *bufio.Reader) { b, _ := r.Peek(1); r.ReadByte(); use(b) }

func resliced(buf *bytes.Buffer) { b := buf.Bytes()[:1]; buf.WriteByte('x'); use(b) }

func readLineTwice(r *bufio.Reader) { l, _, _ := r.ReadLine(); r.ReadLine(); use(l) }

func bothPaths(c bool, buf *bytes.Buffer) {
	var b []byte
	if c {
		b = buf.Bytes()
	} else {
		b = buf.Next(1)
	}
	buf.WriteByte('x')
	use(b)
}

func lengthThenUse(buf *bytes.Buffer) { b := buf.Bytes(); buf.Reset(); _ = len(b); use(b) }

func nextTwice(buf *bytes.Buffer) { buf.Next(1); b := buf.Next(1); use(b) }

func otherOwner(a, b *bytes.Buffer) { v := a.Bytes(); a.WriteByte('x'); b.Reset(); _ = v }

func readAfterAvailable(buf *bytes.Buffer) { b := buf.AvailableBuffer(); buf.Write(nil); buf.ReadByte(); _ = b }

func readAfterReadLine(r *bufio.Reader) { l, _, _ := r.ReadLine(); r.ReadLine(); r.ReadByte(); _ = l }

func lengthOnly(buf *bytes.Buffer) int { b := buf.Bytes(); buf.Reset(); return len(b) + cap(b) }

func passedToTheEvent(buf *bytes.Buffer) { b := buf.AvailableBuffer(); buf.Write(b) }

func usedBefore(buf *bytes.Buffer) { b := buf.Bytes(); use(b); buf.Reset() }

func neverEnded(buf *bytes.Buffer) { b := buf.AvailableBuffer(); buf.ReadByte(); use(b) }

func resultUnused(r *bufio.Reader) { r.Peek(1); r.ReadByte() }

func nilBuffer() { b := (*bytes.Buffer)(nil).Bytes(); (*bytes.Buffer)(nil).Reset(); use(b) }

func keepLast(sc *bufio.Scanner) {
	var last []byte
	for sc.Scan() {
		last = sc.Bytes()
	}
	use(last)
}

func nested(c, d bool, buf *bytes.Buffer) {
	var b []byte
	if c {
		if d {
			b = buf.Bytes()
		} else {
			b = buf.Next(1)
		}
	} else {
		b = buf.Next(2)
	}
	buf.WriteByte('x')
	use(b)
}

func indexed(buf *bytes.Buffer) byte { b := buf.Bytes(); buf.Reset(); return b[0] }

func mixedEventsWritten(c bool, buf *bytes.Buffer) {
	var b []byte
	if c {
		b = buf.Bytes()
	} else {
		b = buf.AvailableBuffer()
	}
	buf.WriteByte('x')
	use(b)
}

func mixedEventsRead(c bool, buf *bytes.Buffer) {
	var b []byte
	if c {
		b = buf.Bytes()
	} else {
		b = buf.AvailableBuffer()
	}
	buf.ReadByte()
	use(b)
}

func mixedOwners(c bool, x, y *bytes.Buffer) {
	var b []byte
	if c {
		b = x.Bytes()
	} else {
		b = y.Bytes()
	}
	x.Reset()
	use(b)
}

func nestedLoops(n int, sc *bufio.Scanner) {
	var last []byte
	for n > 0 {
		for sc.Scan() {
			last = sc.Bytes()
		}
		n--
	}
	use(last)
}

func nestedLoopsMixedOwners(n int, x, y *bufio.Scanner) {
	var last []byte
	for n > 0 {
		for x.Scan() {
			last = x.Bytes()
		}
		last = y.Bytes()
		n--
	}
	x.Scan()
	use(last)
}

func errorOnly(r *bufio.Reader) error { _, err := r.Peek(1); r.ReadByte(); return err }

func viewReleased(buf *bytes.Buffer) { b := buf.Bytes(); release(b); use(b) }

func clearedNext(buf *bytes.Buffer) { hdr := buf.Next(4); buf.Reset(); use(hdr) }

func grownAvailable(buf *bytes.Buffer) { b := buf.AvailableBuffer(); buf.Grow(64); use(b) }

func sameBlockOwner(next func() *bytes.Buffer) {
	var prev []byte
	for {
		buf := next()
		buf.WriteByte('x')
		use(prev)
		prev = buf.Bytes()
	}
}

// nilEachIteration clears last on the back edge: only the first iteration sees the ended view,
// and it does not use it.
func nilEachIteration(n int, sc *bufio.Scanner) {
	last := sc.Bytes()
	sc.Scan()
	for i := 0; i < n; i++ {
		if i > 0 {
			use(last)
		}
		last = nil
	}
}

// firstIterationOnly uses last only in the first iteration, while it is still valid.
func firstIterationOnly(n int, sc *bufio.Scanner) {
	last := sc.Bytes()
	for i := 0; i < n; i++ {
		if i == 0 {
			use(last)
		}
		last = sc.Bytes()
		sc.Scan()
	}
}

type tokens struct{ sc bufio.Scanner }

type outer struct{ in conn }

func fieldReplaced(c *conn) []byte { b := c.buf.Bytes(); c.buf = bytes.Buffer{}; c.buf.WriteString("x"); return b }

func structReplaced(c *conn) []byte { b := c.buf.Bytes(); *c = conn{}; c.buf.WriteString("x"); return b }

func enclosingReplaced(o *outer) []byte { b := o.in.buf.Bytes(); o.in = conn{}; o.in.buf.WriteString("x"); return b }

func localReplaced() []byte {
	var buf bytes.Buffer
	buf.WriteString("x")
	b := buf.Bytes()
	buf = bytes.Buffer{}
	buf.WriteString("y")
	return b
}

func replacedEachIteration(xs []string) {
	var c conn
	var prev []byte
	for _, x := range xs {
		c = conn{}
		c.buf.WriteString(x)
		use(prev)
		prev = c.buf.Bytes()
	}
}

// initialisedFirst stores the buffer only before the view is made, which replaces nothing the
// view points into.
func initialisedFirst(src *bytes.Buffer) {
	var buf bytes.Buffer
	buf = *src
	b := buf.Bytes()
	buf.Reset()
	use(b)
}

// fieldKeepLast keeps the last token of a scanner that a struct holds. The view's owner,
// t.sc, is first addressed in the loop body, but its struct t comes from outside the loop.
func fieldKeepLast(t *tokens) {
	var last []byte
	for t.sc.Scan() {
		last = t.sc.Bytes()
	}
	use(last)
}

func correlatedMerge(fast bool, buf *bytes.Buffer) {
	var data []byte
	if fast {
		data = buf.Bytes()
		buf.Reset()
	}
	if !fast {
		use(data)
	}
}

func newBufferEachIteration(xs []string) {
	var prev []byte
	for _, x := range xs {
		var buf bytes.Buffer
		buf.WriteString(x)
		use(prev)
		prev = buf.Bytes()
	}
}

func rangedBuffers(bufs []*bytes.Buffer) {
	var prev []byte
	for _, buf := range bufs {
		buf.WriteByte('x')
		use(prev)
		prev = buf.Bytes()
	}
}

func scannerEachIteration(rs []*bufio.Reader) {
	var prev []byte
	for _, r := range rs {
		sc := bufio.NewScanner(r)
		sc.Scan()
		use(prev)
		prev = sc.Bytes()
	}
}

func notOnlyViews(c bool, buf *bytes.Buffer) {
	var b []byte
	if c {
		b = buf.Bytes()
	} else {
		b = make([]byte, 1)
	}
	buf.Reset()
	use(b)
}
`

func TestUsesFindViewsUsedAfterTheyEnded(t *testing.T) {
	pkg := buildSource(t, viewSrc)

	for _, tc := range []struct{ name, view, event string }{
		{"reset", "Bytes", "Reset"},
		{"scan", "Bytes", "Scan"},
		{"peek", "Peek", "ReadByte"},
		{"resliced", "Bytes", "WriteByte"},
		{"readLineTwice", "ReadLine", "ReadLine"},
		{"bothPaths", "Bytes", "WriteByte"},
		{"lengthThenUse", "Bytes", "Reset"},
		{"keepLast", "Bytes", "Scan"},
		{"nested", "Bytes", "WriteByte"},
		{"indexed", "Bytes", "Reset"},
		{"mixedEventsWritten", "Bytes", "WriteByte"},
		{"field", "Bytes", "Reset"},
		{"embedded", "Bytes", "Reset"},
		{"fieldKeepLast", "Bytes", "Scan"},
		{"initialisedFirst", "Bytes", "Reset"},
	} {
		uses := analyzeViews(t, pkg, tc.name).Uses()
		if len(uses) != 1 {
			t.Errorf("%s: %d uses, want 1", tc.name, len(uses))
			continue
		}

		u := uses[0]
		if u.View == nil || u.View.Callee.Name() != tc.view || u.Site.Callee.Name() != tc.event || u.Arg != 0 {
			t.Errorf("%s: use attributed to view %v, event %s argument %d; want %s and %s",
				tc.name, u.View, u.Site.Callee.Name(), u.Arg, tc.view, tc.event)
		}
	}

	// len(b) and cap(b) read the header only: the use reported is the call of use after them.
	if u := analyzeViews(t, pkg, "lengthThenUse").Uses()[0]; !callsUse(u.Instr) {
		t.Errorf("lengthThenUse: the use is %v, want the call of use", u.Instr)
	}
}

func TestUsesIgnoreViewsStillValid(t *testing.T) {
	pkg := buildSource(t, viewSrc)

	for _, name := range []string{
		"nextTwice", "otherOwner", "readAfterAvailable", "readAfterReadLine", "lengthOnly",
		"passedToTheEvent", "usedBefore", "mixedEventsRead", "mixedOwners", "notOnlyViews",
		"nestedLoopsMixedOwners", "errorOnly", "clearedNext", "correlatedMerge",
		"newBufferEachIteration", "rangedBuffers", "scannerEachIteration", "sameBlockOwner",
		"connEachIteration", "nilEachIteration", "firstIterationOnly", "fieldReplaced", "structReplaced",
		"enclosingReplaced", "localReplaced", "replacedEachIteration",
	} {
		if uses := usesOf(pkg, name); len(uses) != 0 {
			t.Errorf("%s: %d uses, want 0", name, len(uses))
		}
	}
}

// TestAViewTakenBackIsAnInvalidation covers a view passed to an invalidating call: the use is
// after an invalidation, MS301, not after a view ended.
func TestAViewTakenBackIsAnInvalidation(t *testing.T) {
	uses := analyzeViews(t, buildSource(t, viewSrc), "viewReleased").Uses()
	if len(uses) != 1 || uses[0].View != nil || uses[0].Site.Callee.Name() != "release" {
		t.Fatalf("uses = %+v, want one use after release with no view", uses)
	}
}

// TestMissesViewsKeptAcrossNestedLoops records a known miss: the outer loop's phi merges nil
// from the entry with the inner loop's view, and the nil keeps the merge valid. Following it
// takes a special case in the dataflow that the benchmark corpus has shown no need for.
func TestMissesViewsKeptAcrossNestedLoops(t *testing.T) {
	a := analyzeViews(t, buildSource(t, viewSrc), "nestedLoops")
	if uses := a.Uses(); len(uses) != 0 {
		t.Fatalf("%d uses; the miss is fixed, so move nestedLoops to the reported cases", len(uses))
	}
}

// TestMissesOwnersLoadedFromMemory records known misses: a buffer behind a pointer field, and
// one that a function literal captures, are loaded at each call and could be reassigned between
// the calls, so they are not followed. Each load is a different owner, so no call ends a view
// and there is no analysis at all. The control, reset, makes the same calls on a parameter and
// is reported, so the test cannot pass because views stopped being tracked.
func TestMissesOwnersLoadedFromMemory(t *testing.T) {
	pkg := buildSource(t, viewSrc)

	if uses := analyzeViews(t, pkg, "reset").Uses(); len(uses) != 1 {
		t.Fatalf("control: reset has %d uses, want 1", len(uses))
	}

	for _, fn := range []*ssa.Function{pkg.Func("behindPointer"), pkg.Func("captured").AnonFuncs[0]} {
		var eng flow.Engine

		if a := invalidation.AnalyzeWithViews(fn, lookup, &eng); a != nil && len(a.Uses()) != 0 {
			t.Errorf("%s: %d uses; the miss is fixed, so move it to the reported cases", fn, len(a.Uses()))
		}
	}
}

// TestViewsNeedAnEndingEvent covers functions with a view that nothing ends, or no view at all,
// such as a view of a constant nil buffer: there is nothing to solve.
func TestViewsNeedAnEndingEvent(t *testing.T) {
	pkg := buildSource(t, viewSrc)

	for _, name := range []string{"neverEnded", "resultUnused", "grownAvailable", "otherConn", "otherField", "nilBuffer"} {
		var eng flow.Engine

		if a := invalidation.AnalyzeWithViews(pkg.Func(name), lookup, &eng); a != nil {
			t.Errorf("%s: got an analysis, want nil", name)
		}
	}
}

// TestViewsLeaveInvalidationsAlone runs AnalyzeWithViews on functions without views: the uses
// are the same as Analyze finds, with no view.
func TestViewsLeaveInvalidationsAlone(t *testing.T) {
	pkg := build(t)

	for _, name := range []string{"straight", "bothPhi"} {
		uses := analyzeViews(t, pkg, name).Uses()
		if len(uses) != 1 || uses[0].View != nil || uses[0].Site.Callee.Name() != "release" {
			t.Errorf("%s: uses = %+v, want one use after release with no view", name, uses)
		}
	}
}

func TestAnalyzeTracksNoViews(t *testing.T) {
	pkg := buildSource(t, viewSrc)

	var eng flow.Engine

	if a := invalidation.Analyze(pkg.Func("reset"), lookup, &eng); a != nil {
		t.Error("Analyze tracked a view; only AnalyzeWithViews does")
	}
}

func callsUse(instr ssa.Instruction) bool {
	call, ok := instr.(*ssa.Call)
	if !ok {
		return false
	}

	fn := call.Common().StaticCallee()

	return fn != nil && fn.Name() == "use"
}

// usesOf returns the uses AnalyzeWithViews finds in the function name, none when there is
// nothing to analyse.
func usesOf(pkg *ssa.Package, name string) []invalidation.Use {
	var eng flow.Engine

	if a := invalidation.AnalyzeWithViews(pkg.Func(name), lookup, &eng); a != nil {
		return a.Uses()
	}

	return nil
}

func analyzeViews(t *testing.T, pkg *ssa.Package, name string) *invalidation.Analysis {
	t.Helper()

	var eng flow.Engine

	a := invalidation.AnalyzeWithViews(pkg.Func(name), lookup, &eng)
	if a == nil {
		t.Fatalf("%s: no analysis", name)
	}

	return a
}
