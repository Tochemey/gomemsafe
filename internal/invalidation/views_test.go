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
	} {
		if uses := analyzeViews(t, pkg, name).Uses(); len(uses) != 0 {
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
// from the entry with the inner loop's view, and nil keeps the merge valid. Skipping the nil
// would report correlated branches such as correlatedMerge, which are correct.
func TestMissesViewsKeptAcrossNestedLoops(t *testing.T) {
	if uses := analyzeViews(t, buildSource(t, viewSrc), "nestedLoops").Uses(); len(uses) != 0 {
		t.Fatalf("%d uses; the miss is fixed, so move nestedLoops to the reported cases", len(uses))
	}
}

// TestViewsNeedAnEndingEvent covers functions with a view that nothing ends, or no view at all:
// there is nothing to solve.
func TestViewsNeedAnEndingEvent(t *testing.T) {
	pkg := buildSource(t, viewSrc)

	for _, name := range []string{"neverEnded", "resultUnused", "grownAvailable"} {
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

func analyzeViews(t *testing.T, pkg *ssa.Package, name string) *invalidation.Analysis {
	t.Helper()

	var eng flow.Engine

	a := invalidation.AnalyzeWithViews(pkg.Func(name), lookup, &eng)
	if a == nil {
		t.Fatalf("%s: no analysis", name)
	}

	return a
}
