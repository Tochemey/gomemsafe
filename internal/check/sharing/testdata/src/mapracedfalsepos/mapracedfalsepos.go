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

// Package mapracedfalsepos has goroutines that share a captured map correctly, and look-alikes of
// the map races MS402 reports. MS402 must stay silent on all of them. Where the variable itself
// is replaced while a goroutine runs, that race is MS401's, reported once.
package mapracedfalsepos

import "sync"

func use(...any) {}

type registry struct {
	byName map[string]int
	byID   map[int]string
}

func waited() int {
	var wg sync.WaitGroup

	m := map[string]int{}

	wg.Add(1)

	go func() {
		defer wg.Done()

		m["a"] = 1
	}()

	wg.Wait()

	return m["a"]
}

func locked() {
	var mu sync.Mutex

	m := map[string]int{}

	go func() {
		mu.Lock()
		m["a"] = 1
		mu.Unlock()
	}()

	mu.Lock()
	m["b"] = 2
	mu.Unlock()
}

func onlyReads() {
	m := map[string]int{"a": 1}

	go func() { use(m["a"]) }()

	use(m["a"], len(m))
}

func otherMapField() {
	r := registry{byName: map[string]int{}, byID: map[int]string{}}

	go func() { r.byName["a"] = 1 }()

	r.byID[1] = "a"
}

func writtenBeforeStart() {
	m := map[string]int{}
	m["a"] = 1

	go func() { m["b"] = 2 }()
}

func replacedInStarter() {
	m := map[string]int{}

	go func() { use(m["a"]) }()

	m = map[string]int{} // want `MS401: m written here while a goroutine reads it`
	m["a"] = 1
}

func replacedInGoroutine() {
	m := map[string]int{"a": 1}

	go func() {
		m = map[string]int{}
		m["b"] = 2
	}()

	m["c"] = 3 // want `MS401: m read here while a goroutine writes it`
}

func copiedBeforeStart() {
	m := map[string]int{}
	old := m

	m = map[string]int{}

	go func() { m["a"] = 1 }()

	old["b"] = 2
}

func passedOn(fill func(map[string]int)) {
	m := map[string]int{}

	go func() { fill(m) }()

	use(len(m))
}

func syncMap() {
	var m sync.Map

	go func() { m.Store("a", 1) }()

	m.Load("a")
}

func sliceNotMap() {
	s := []int{1, 2}

	go func() { s[0] = 3 }()

	use(len(s))
}
