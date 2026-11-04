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

// Package escapes names parameters in every place their memory could leave the function, and
// has look-alikes that only read, compare, or write into them, or name values the call copies.
package escapes

import (
	"context"
	"io"
	"unsafe"
)

type T struct {
	n    int
	next *T
}

type holder struct{ t *T }

type sink interface{ Put(*T) }

var global *T

var ch = make(chan *T)

func returned(t *T) *T { return t } // want `escapes: stored`

func returnedField(t *T) int { return t.n }

func returnedNext(t *T) *T { return t.next } // want `escapes: stored`

func builtByKey(t *T) map[int]*T { return map[int]*T{0: t} } // want `escapes: stored`

func numberSent(t *T, c chan int) { c <- t.n }

func sent(t *T) { ch <- t } // want `escapes: stored`

func started(t *T) { go use(t) } // want `escapes: stored, escapes.use`

func built(t *T) { _ = holder{t: t} } // want `escapes: stored`

func captured(t *T) { _ = func() int { return t.n } } // want `escapes: stored`

func assigned(t *T) { global = t } // want `escapes: stored`

func declared(t *T) { // want `escapes: stored`
	var y = t
	global = y
}

func mapKey(m map[*T]int, t *T) { m[t] = 1 } // want `escapes: stored`

func mapValue(m map[int]*T, n int) { m[n] = nil }

func addressed(t T) { _ = &t } // want `escapes: stored`

func copied(dst, src []*T) { copy(dst, src) } // want `escapes: stored`

func copiedBytes(dst, src []byte) { copy(dst, src) }

func appended(items []*T, t *T) { _ = append(items, t) } // want `escapes: stored`

func appendedBytes(dst, src []byte) { _ = append(dst, src...) } // want `escapes: stored`

func appendedLocally(dst, src []byte) int { return len(append(dst, src...)) }

func sliced(p *byte) []byte { return unsafe.Slice(p, 1) } // want `escapes: stored`

func slicedLocally(p *byte) int { return len(unsafe.Slice(p, 1)) }

func keyed(t *T) { use2(holder{t: t}) } // want `escapes: stored, escapes.use2`

func use2(holder) {}

func panicked(t *T) { panic(t) } // want `escapes: stored`

func literalCalled(t *T) { func(x *T) { global = x }(t) } // want `escapes: stored`

func ranged(ts []*T) { // want `escapes: stored`
	for range ts {
	}
}

func rangedKeys(m map[*T]int) { // want `escapes: stored`
	for range m {
	}
}

func rangedArray(a *[2]*T) { // want `escapes: stored`
	for range a {
	}
}

func rangedChan(c chan *T) { // want `escapes: stored`
	for range c {
	}
}

func rangedFunc(seq func(func(*T) bool)) { // want `escapes: stored`
	for range seq {
	}
}

func rangedString(s string) int {
	n := 0
	for range s {
		n++
	}

	return n
}

func rangedNumbers(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}

	return total
}

func passed(t *T) { use(t) } // want `escapes: escapes.use`

func passedField(t *T) { use(t.next) } // want `escapes: escapes.use`

func nested(t *T) { use(gives(t)) } // want `escapes: escapes.gives \(result used\)`

func givenAway(t *T) { global = gives(t) } // want `escapes: escapes.gives \(result used\)`

func measured(t *T) int { return len(slice(t)) } // want `escapes: escapes.slice`

func receiverOfResult(t *T) { gives(t).method() } // want `escapes: escapes.gives \(result used\)`

func dynamicResult(s sink, t *T) { s.Put(gives(t)) } // want `escapes: dynamic, escapes.gives \(result used\)`

func slice(t *T) []*T { return []*T{t} } // want `escapes: stored`

func converted(t *T) { global = (*T)(t) } // want `escapes: stored`

func gives(t *T) *T { return t } // want `escapes: stored`

func givesTwo(t *T) (*T, error) { return t, nil } // want `escapes: stored`

func countTwo(t *T) (int, error) { return t.n, nil }

func returnedTwo(t *T) (*T, error) { return givesTwo(t) } // want `escapes: escapes.givesTwo \(result used\)`

func countedTwo(t *T) (int, error) { return countTwo(t) } // want `escapes: escapes.countTwo`

func receiver(t *T) { t.method() } // want `escapes: \(\*escapes.T\).method`

func (t *T) method() {}

func use(*T) {}

func dynamic(s sink, t *T) { s.Put(t) } // want `escapes: dynamic`

func valueCalled(f func(*T), t *T) { f(t) } // want `escapes: dynamic`

func valueOnly(f func()) { f() }

func known(w io.Writer, b []byte) { _, _ = w.Write(b) }

func contextDone(ctx context.Context) { <-ctx.Done() }

func builtinsReading(ts []*T, m map[*T]int, c chan *T) int {
	close(c)
	delete(m, nil)
	clear(ts)

	return len(ts) + cap(ts)
}

func compared(t *T) bool { return t == nil }

func fieldAddressed(t *T) *int { return &t.n } // want `escapes: stored`

func switched(x any) { // want `escapes: stored`
	switch y := x.(type) {
	case *T:
		global = y
	}
}

func written(t *T) { t.n = 1 }

func onlyRead(t *T) {
	if t.n > 0 {
		t.n--
	}
}

func received(c chan *T) { <-c }

func copiedValue(n int, s string) int { return n + len(s) }

func unnamed(*T) {}

func localOnly() *T {
	x := &T{}
	return x
}
