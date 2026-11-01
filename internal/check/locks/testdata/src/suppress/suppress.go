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

// Package suppress has breaches of lock contracts that a //memsafe:allow directive, a
// +checklocksforce annotation, or a +checklocksignore annotation keeps silent.
package suppress

import "sync"

type counter struct {
	mu sync.Mutex
	// +checklocks:mu
	n int
}

// +checklocks:c.mu
func requires(c *counter) { c.n++ }

// +checklocksacquire:c.mu
func acquires(c *counter) { c.mu.Lock() }

func allowed(c *counter) {
	//memsafe:allow MS403 the counter is not shared yet, so the lock is not needed
	c.n = 1
}

func forced(c *counter) {
	c.n = 1 // +checklocksforce: the caller holds c.mu
	c.n = 2
	c.mu.Unlock()
	requires(c) // +checklocksforce
	c.n = 3
	c.mu.Unlock()
	c.mu.Unlock() // +checklocksforce
	acquires(c)
	acquires(c) // +checklocksforce
	c.mu.Unlock()
	c.mu.Unlock() // +checklocksforce
	c.mu.Lock()
	c.mu.Lock() // +checklocksforce
	c.mu.Unlock()
}

func forcedReturn(c *counter) { // +checklocksforce: the lock is handed to the caller
	c.mu.Lock()
}

// +checklocksacquire:c.mu
func forcedAcquire(c *counter) {
	return // +checklocksforce
}

func ignoredLine(c *counter) {
	c.n = 1 // +checklocksignore
}

// +checklocksignore
func ignoredFunction(c *counter) {
	c.n = 1
	c.mu.Unlock()
}
