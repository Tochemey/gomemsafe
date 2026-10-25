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
	"go/types"
	"slices"
	"testing"

	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/effect"
	"github.com/tochemey/gomemsafe/internal/flow"
	"github.com/tochemey/gomemsafe/internal/invalidation"
)

// returnSrc declares functions whose summaries return views or end them.
const returnSrc = `package p

import (
	"bufio"
	"bytes"
)

type conn struct{ buf bytes.Buffer }

func contents(buf *bytes.Buffer) []byte { return buf.Bytes() }

func head(buf *bytes.Buffer) []byte { return buf.Bytes()[:1] }

func second(_ int, buf *bytes.Buffer) []byte { return buf.Bytes() }

func line(r *bufio.Reader) ([]byte, error) { return r.ReadSlice('\n') }

func nilOrView(buf *bytes.Buffer) []byte {
	if buf.Len() == 0 {
		return nil
	}
	return buf.Bytes()
}

func merged(c bool, buf *bytes.Buffer) []byte {
	if c {
		return buf.Bytes()
	}
	return buf.AvailableBuffer()
}

func nothingEndsBoth(c bool, r *bufio.Reader) []byte {
	if c {
		l, _, _ := r.ReadLine()
		return l
	}
	b, _ := r.Peek(1)
	return b
}

func either(c bool, a, b *bytes.Buffer) []byte {
	if c {
		return a.Bytes()
	}
	return b.Bytes()
}

func notOnlyViews(c bool, buf *bytes.Buffer) []byte {
	if c {
		return buf.Bytes()
	}
	return make([]byte, 1)
}

func noResult(buf *bytes.Buffer) { _ = buf.Bytes() }

func noView(buf *bytes.Buffer) []byte { return nil }

func onlyNil(buf *bytes.Buffer) []byte { _ = buf.Bytes(); return nil }

func (c *conn) bytes() []byte { return c.buf.Bytes() }

func local() []byte {
	var buf bytes.Buffer
	return buf.Bytes()
}

func replacedView(buf *bytes.Buffer) []byte {
	b := buf.Bytes()
	*buf = bytes.Buffer{}
	return b
}

func clear(buf *bytes.Buffer) { buf.Reset() }

func fill(_ int, buf *bytes.Buffer) { buf.WriteByte('x') }

func clearAndFill(buf *bytes.Buffer) { buf.Reset(); buf.WriteByte('x') }

func readLine(r *bufio.Reader) { r.ReadLine() }

func clearedBothWays(c bool, buf *bytes.Buffer) {
	if c {
		buf.Reset()
	} else {
		buf.Truncate(0)
	}
}

func differentEvents(c bool, buf *bytes.Buffer) {
	if c {
		buf.Reset()
	} else {
		buf.WriteByte('x')
	}
}

func maybeCleared(c bool, buf *bytes.Buffer) {
	if c {
		buf.Reset()
	}
}

func (c *conn) reset() { c.buf.Reset() }

func renewed(buf *bytes.Buffer) {
	*buf = bytes.Buffer{}
	buf.Reset()
}

func noEvents(buf *bytes.Buffer) int { return buf.Len() }

func clearedForever(buf *bytes.Buffer) {
	buf.Reset()
	for {
	}
}
`

func TestViewAtReturnFindsViewsIntoParameters(t *testing.T) {
	pkg := buildSource(t, returnSrc)
	bytesView := effect.Read | effect.Write | effect.Clear

	for _, tc := range []struct {
		name string
		want effect.View
	}{
		{"contents", effect.View{Arg: 0, EndedBy: bytesView}},
		{"head", effect.View{Arg: 0, EndedBy: bytesView}},
		{"second", effect.View{Arg: 1, EndedBy: bytesView}},
		{"line", effect.View{Arg: 0, EndedBy: effect.Read}},
		{"nilOrView", effect.View{Arg: 0, EndedBy: bytesView}},
		{"merged", effect.View{Arg: 1, EndedBy: effect.Write}},
	} {
		if got, ok := invalidation.ViewAtReturn(pkg.Func(tc.name), lookup); !ok || got != tc.want {
			t.Errorf("%s: ViewAtReturn = %+v, %v; want %+v", tc.name, got, ok, tc.want)
		}
	}
}

func TestViewAtReturnRejectsWhatIsNoViewOfOneParameter(t *testing.T) {
	pkg := buildSource(t, returnSrc)

	fns := []*ssa.Function{connMethod(pkg, "bytes")}
	for _, name := range []string{
		"nothingEndsBoth", "either", "notOnlyViews", "noResult", "noView", "onlyNil", "local", "replacedView",
	} {
		fns = append(fns, pkg.Func(name))
	}

	for _, fn := range fns {
		if got, ok := invalidation.ViewAtReturn(fn, lookup); ok {
			t.Errorf("%s: ViewAtReturn = %+v, want no view", fn.Name(), got)
		}
	}
}

func TestEventsAtReturnFindsEventsOnEveryPath(t *testing.T) {
	pkg := buildSource(t, returnSrc)

	for _, tc := range []struct {
		name string
		want []effect.Event
	}{
		{"clear", []effect.Event{effect.Clear}},
		{"fill", []effect.Event{0, effect.Write}},
		{"clearAndFill", []effect.Event{effect.Clear | effect.Write}},
		{"readLine", []effect.Event{effect.Read | effect.LineRead}},
		{"clearedBothWays", []effect.Event{0, effect.Clear}},
	} {
		var eng flow.Engine

		if got := invalidation.EventsAtReturn(pkg.Func(tc.name), lookup, &eng); !slices.Equal(got, tc.want) {
			t.Errorf("%s: EventsAtReturn = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestEventsAtReturnIgnoresEventsNotOnEveryPathOrNotOnAParameter(t *testing.T) {
	pkg := buildSource(t, returnSrc)

	fns := []*ssa.Function{connMethod(pkg, "reset")}
	for _, name := range []string{"differentEvents", "maybeCleared", "renewed", "noEvents", "clearedForever"} {
		fns = append(fns, pkg.Func(name))
	}

	for _, fn := range fns {
		var eng flow.Engine

		if got := invalidation.EventsAtReturn(fn, lookup, &eng); got != nil {
			t.Errorf("%s: EventsAtReturn = %v, want none", fn.Name(), got)
		}
	}
}

func TestEventsAtReturnGivesUpPastBudget(t *testing.T) {
	eng := flow.Engine{Budget: 1}

	if got := invalidation.EventsAtReturn(buildSource(t, returnSrc).Func("clearedBothWays"), lookup, &eng); got != nil {
		t.Fatalf("EventsAtReturn = %v past the budget, want none", got)
	}

	if eng.BudgetHits != 1 {
		t.Fatalf("BudgetHits = %d, want 1", eng.BudgetHits)
	}
}

// connMethod returns the method of *conn named name.
func connMethod(pkg *ssa.Package, name string) *ssa.Function {
	return pkg.Prog.LookupMethod(types.NewPointer(pkg.Type("conn").Type()), pkg.Pkg, name)
}
