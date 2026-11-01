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

// Package locks declares checklocks guards and contracts, which become facts.
package locks

import (
	"bytes"
	"sync"
)

// Mu guards Count.
var Mu sync.Mutex // want Mu:`^lock$`

// Count is guarded by Mu.
//
// +checklocks:Mu
var Count int // want Count:`^guards\(Mu\)$`

// Counter is a guarded counter.
type Counter struct {
	mu sync.RWMutex // want mu:`^lock$`
	// +checklocks:mu
	n int // want n:`^guards\(mu\)$`
	// +checklocks:mu
	// +checklocks:Mu
	both int // want both:`^guards\(mu, Mu\)$`
	plain int
}

// Box is a generic guarded box.
type Box[T any] struct {
	mu sync.Mutex // want mu:`^lock$`
	// +checklocks:mu
	value T // want value:`^guards\(mu\)$`
}

// Inc needs the lock.
//
// +checklocks:c.mu
func (c *Counter) Inc() { c.n++ } // want Inc:`^locks\(requires: c.mu\)$`

// Get needs the lock for reading.
//
// +checklocksread:c.mu
func (c *Counter) Get() int { return c.n } // want Get:`^locks\(requires: c.mu \(read\)\)$`

// Lock locks the counter for its caller.
//
// +checklocksacquire:c.mu
func (c *Counter) Lock() { c.mu.Lock() } // want Lock:`^syncs locks\(acquires: c.mu\)$`

// Unlock unlocks the counter its caller locked.
//
// +checklocksrelease:c.mu
func (c *Counter) Unlock() { c.mu.Unlock() } // want Unlock:`^locks\(releases: c.mu\)$`

// Reset must not be called with the lock held, nor with Mu.
//
// +checklocksexclude:c.mu
// +checklocksexclude:Mu
func (c *Counter) Reset() { // want Reset:`^syncs locks\(excludes: c.mu, Mu\)$`
	c.mu.Lock()
	c.n = 0
	c.mu.Unlock()
}

// Drain has a contract and a derived summary: it ends the views of b too.
//
// +checklocks:c.mu
func (c *Counter) Drain(b *bytes.Buffer) { b.Reset() } // want Drain:`^ends\(1: 0x4\) locks\(requires: c.mu\)$`

// Unchecked is not checked, but its callers are.
//
// +checklocks:c.mu
// +checklocksignore
func (c *Counter) Unchecked() { c.n = 0 } // want Unchecked:`^locks\(requires: c.mu; ignore\)$`

// Plain has no contract.
func (c *Counter) Plain() {}
