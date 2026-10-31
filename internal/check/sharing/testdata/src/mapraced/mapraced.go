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

// Package mapraced has goroutines that operate on a captured map while the function that starts
// them does too, with nothing between the two that may order them. MS402 must report each one.
package mapraced

func use(...any) {}

type registry struct {
	byName map[string]int
	byID   map[int]string
}

func readAfterWrite() {
	m := map[string]int{}

	go func() { m["a"] = 1 }()

	_ = m["a"] // want `MS402: map m read here while a goroutine writes it, with no synchronisation between them`
}

func writeAfterRead() {
	m := map[string]int{"a": 1}

	go func() { use(m["a"]) }()

	m["b"] = 2 // want `MS402: map m written here while a goroutine reads it`
}

func bothWrite() {
	m := map[string]int{}

	go func() { m["a"] = 1 }()

	m["b"] = 2 // want `MS402: map m written here while a goroutine writes it`
}

func deletes() {
	m := map[string]int{"a": 1}

	go func() { delete(m, "a") }()

	use(len(m)) // want `MS402: map m read here while a goroutine writes it`
}

func clears() {
	m := map[string]int{"a": 1}

	go func() { clear(m) }()

	for k := range m { // want `MS402: map m read here while a goroutine writes it`
		use(k)
	}
}

func rangesInGoroutine() {
	m := map[string]int{"a": 1}

	go func() {
		for k, v := range m {
			use(k, v)
		}
	}()

	m["b"] = 2 // want `MS402: map m written here while a goroutine reads it`
}

func increments() {
	counts := map[string]int{}

	go func() { counts["a"]++ }()

	counts["a"]++ // want `MS402: map counts read here while a goroutine writes it`
}

func field() {
	r := registry{byName: map[string]int{}}

	go func() { r.byName["a"] = 1 }()

	_ = r.byName["a"] // want `MS402: map r.byName read here while a goroutine writes it`
}

func commaOk() {
	m := map[string]int{}

	go func() { m["a"] = 1 }()

	if _, ok := m["a"]; ok { // want `MS402: map m read here while a goroutine writes it`
		use()
	}
}
