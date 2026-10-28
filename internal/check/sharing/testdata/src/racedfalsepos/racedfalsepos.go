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

// Package racedfalsepos has goroutines that share captured variables correctly, and look-alikes
// of the races MS401 reports. MS401 must stay silent on all of them.
package racedfalsepos

import (
	"context"
	"sync"
	"sync/atomic"

	"waitlib"
)

type result struct {
	n   int
	err error
}

type waiter interface{ Wait() }

func work() error { return nil }

func use(...any) {}

func waitGroup() error {
	var (
		wg  sync.WaitGroup
		err error
	)

	wg.Add(1)

	go func() {
		defer wg.Done()

		err = work()
	}()

	wg.Wait()

	return err
}

func receive() error {
	var err error

	done := make(chan struct{})

	go func() {
		err = work()

		close(done)
	}()

	<-done

	return err
}

func selects(ctx context.Context) error {
	var err error

	done := make(chan struct{})

	go func() {
		defer close(done)

		err = work()
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}

	return err
}

func mutexBothSides() int {
	var mu sync.Mutex

	n := 0

	go func() {
		mu.Lock()
		n++
		mu.Unlock()
	}()

	mu.Lock()
	defer mu.Unlock()

	return n
}

func goroutineWaitsFirst() {
	n := 0
	start := make(chan struct{})

	go func() {
		<-start
		use(n)
	}()

	n = 1

	close(start)
}

func throughFact() error {
	var (
		g   waitlib.Group
		res int
	)

	g.Go(func() error { res = 1; return nil })

	err := g.Wait()
	use(res)

	go func() { res = 2 }()

	_ = g.Wait()
	use(res)

	return err
}

func throughHelper(g *waitlib.Group) {
	n := 0

	go func() { n = 1 }()

	wait(g)

	use(n)
}

func throughInterface(w waiter) {
	n := 0

	go func() { n = 1 }()

	w.Wait()

	use(n)
}

func throughFunctionValue(wait func()) {
	n := 0

	go func() { n = 1 }()

	wait()

	use(n)
}

func throughLiteral(wg *sync.WaitGroup) {
	n := 0

	go func() { n = 1 }()

	func() { wg.Wait() }()

	use(n)
}

func atomics() {
	var done atomic.Bool

	n := 0

	go func() {
		n = 1

		done.Store(true)
	}()

	for !done.Load() {
	}

	use(n)
}

func deferredWait() (err error) {
	var wg sync.WaitGroup

	defer wg.Wait()

	wg.Add(1)

	go func() {
		defer wg.Done()

		err = work()
	}()

	return
}

func onlyReads() {
	n := 1

	go func() { use(n) }()

	use(n)
}

func otherFields() {
	var r result

	go func() { r.err = work() }()

	r.n = 1
}

func accessedBeforeStart() error {
	var err error

	err = work()
	use(err)

	go func() { err = work() }()

	return nil
}

func pointerTarget() {
	r := &result{}

	go func() { r.n = 1 }()

	use(r)
}

func freshEachIteration(items []int) {
	for _, item := range items {
		cur := item

		go func() { cur++ }()
	}
}

func loopVariable(n int) {
	for i := range n {
		go func() { use(i) }()
	}

	for i := 0; i < n; i++ {
		go func() { use(i) }()
	}
}

func syncedEachIteration(items []int) int {
	total := 0

	var wg sync.WaitGroup

	for _, item := range items {
		wg.Add(1)

		go func() {
			defer wg.Done()

			total = item
		}()

		wg.Wait()

		use(total)
	}

	return total
}

func addressOnly() {
	var wg sync.WaitGroup

	wg.Add(1)

	go func() { wg.Done() }()

	wg.Wait()
}

func namedFunction(n *int) {
	go work()

	go func(x int) { use(x) }(*n)

	*n = 2
}

func literalAccessesBeforeStart() {
	x := 0

	func() {
		use(x)

		x = 1

		go func() { x = 2 }()
	}()
}

func wait(g *waitlib.Group) { _ = g.Wait() }
