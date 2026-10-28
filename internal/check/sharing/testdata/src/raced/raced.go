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

// Package raced has goroutines that share a captured variable with the function that starts
// them, with nothing between the two accesses that may order them. MS401 must report each one.
package raced

import (
	"sync"

	"waitlib"
)

type result struct {
	n   int
	err error
}

func work() error { return nil }

func use(...any) {}

func readAfterStart() error {
	var err error

	go func() { err = work() }()

	if err != nil { // want `MS401: err read here while a goroutine writes it, with no synchronisation between them`
		return err
	}

	return nil
}

func writeAfterStart() {
	n := 0

	go func() { use(n) }()

	n = 1 // want `MS401: n written here while a goroutine reads it`
}

func bothWrite() {
	n := 0

	go func() { n++ }()

	n++ // want `MS401: n read here while a goroutine writes it`
}

func waitedTooLate() error {
	var (
		wg  sync.WaitGroup
		err error
	)

	wg.Add(1)

	go func() {
		defer wg.Done()

		err = work()
	}()

	waitlib.Log(err) // want `MS401: err read here while a goroutine writes it`
	wg.Wait()

	return err
}

func waitedOnOnePath(c bool) error {
	var wg sync.WaitGroup

	var err error

	wg.Add(1)

	go func() {
		defer wg.Done()

		err = work()
	}()

	if c {
		wg.Wait()
	}

	return err // want `MS401: err read here while a goroutine writes it`
}

func releasesOnly() int {
	var mu sync.Mutex

	n := 0

	go func() { n = 1 }()

	mu.Unlock()

	return n // want `MS401: n read here while a goroutine writes it`
}

func field() {
	var r result

	go func() { r.err = work() }()

	use(r.err) // want `MS401: r.err read here while a goroutine writes it`
}

func wholeStruct() {
	var r result

	go func() { r.n = 1 }()

	use(r) // want `MS401: r read here while a goroutine writes it`
}

func nextIteration(items []int) {
	cur := 0

	for _, item := range items {
		cur = item // want `MS401: cur written here while a goroutine reads it`

		go func() { use(cur) }()
	}
}

func goroutineWritesFirst() error {
	var err error

	done := make(chan struct{})

	go func() {
		err = work()
		<-done
	}()

	use(err) // want `MS401: err read here while a goroutine writes it`
	close(done)

	return nil
}

func inLiteral() {
	run := func() {
		var err error

		go func() { err = work() }()

		use(err) // want `MS401: err read here while a goroutine writes it`
	}

	run()
}

func capturedTwice() {
	x := 0

	go func() { x = 1 }()
	go func() { x = 2 }()

	use(x) // want `MS401: x read here while a goroutine writes it`
	use(x)
}

func outer() {
	x := 0

	func() {
		go func() { x = 1 }()

		use(x) // want `MS401: x read here while a goroutine writes it`
	}()
}

func literalWithoutSync() {
	n := 0

	go func() { n = 1 }()

	func() { use() }()

	use(n) // want `MS401: n read here while a goroutine writes it`
}
