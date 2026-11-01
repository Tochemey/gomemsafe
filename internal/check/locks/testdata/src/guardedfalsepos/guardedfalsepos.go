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

// Package guardedfalsepos honours the lock contracts its checklocks annotations declare, and
// has look-alikes of the breaches MS403 reports. MS403 must stay silent on all of it.
package guardedfalsepos

import "sync"

var globalMu sync.Mutex

// +checklocks:globalMu
var total int

type counter struct {
	mu sync.Mutex
	rw sync.RWMutex
	// +checklocks:mu
	n int
	// +checklocks:rw
	readMostly int
	// +checklocks:mu
	// +checklocks:rw
	both int
	// +checklocks:mu
	nested struct {
		a, b int
	}
}

type nested struct {
	inner counter
	ptr   *counter
	// +checklocks:inner.mu
	viaValue int
	// +checklocks:ptr.mu
	viaPointer int
}

// fancyMutex has gVisor's nested and downgrade forms.
type fancyMutex struct{ m sync.RWMutex }

func (m *fancyMutex) NestedLock()    { m.m.Lock() }
func (m *fancyMutex) NestedUnlock()  { m.m.Unlock() }
func (m *fancyMutex) RUnlock()       { m.m.RUnlock() }
func (m *fancyMutex) DowngradeLock() {}

type fancy struct {
	mu fancyMutex
	// +checklocks:mu
	n int
}

func use(...any) {}

func locked(c *counter) {
	c.mu.Lock()
	c.n = 1
	c.nested.a = 1
	c.mu.Unlock()
}

func readLocked(c *counter) int {
	c.rw.RLock()
	defer c.rw.RUnlock()

	return c.readMostly
}

func bothLocked(c *counter) {
	c.mu.Lock()
	c.rw.Lock()
	c.both = 1
	c.rw.Unlock()
	c.mu.Unlock()
}

func consistentBranches(c *counter, cond bool) {
	if cond {
		c.mu.Lock()
	} else {
		c.mu.Lock()
	}

	c.n = 1

	if cond {
		c.mu.Unlock()
	} else {
		c.mu.Unlock()
	}
}

func loopBody(c *counter, n int) {
	for range n {
		c.mu.Lock()
		c.n++
		c.mu.Unlock()
	}

	c.mu.Lock()

	for range n {
		c.n++
	}

	c.mu.Unlock()
}

func conditionalDefer(c *counter, cond bool) {
	if cond {
		c.mu.Lock()
		defer c.mu.Unlock()
	}

	use(c)
}

func nestedValue(x *nested) {
	x.inner.mu.Lock()
	x.viaValue = 1
	x.inner.mu.Unlock()
	x.ptr.mu.Lock()
	x.viaPointer = 1
	x.ptr.mu.Unlock()
}

func fancyLocks(f *fancy) {
	f.mu.NestedLock()
	f.n = 1
	f.mu.DowngradeLock()
	_ = f.n
	f.mu.RUnlock()
}

func globals() {
	globalMu.Lock()
	total++
	globalMu.Unlock()
}

// +checklocks:c.mu
func requires(c *counter) { c.n++ }

// +checklocksread:c.rw
func requiresRead(c *counter) int { return c.readMostly }

// +checklocksexclude:c.mu
func excludes(c *counter) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

// +checklocksacquire:c.mu
func acquires(c *counter) { c.mu.Lock() }

// +checklocksrelease:c.mu
func releases(c *counter) { c.mu.Unlock() }

// +checklocksacquire:result.mu
func acquiresResult() (result *counter) {
	result = new(counter)
	result.mu.Lock()

	return result
}

// +checklocks:c.mu
// +checklocksignore
func ignored(c *counter) {
	c.n = 1
	c.mu.Unlock()
}

func calls(c *counter) {
	excludes(c)
	acquires(c)
	requires(c)
	c.n = 1
	releases(c)
	c.rw.RLock()
	use(requiresRead(c))
	c.rw.RUnlock()
	c.rw.Lock()
	use(requiresRead(c))
	c.rw.Unlock()

	r := acquiresResult()
	r.n = 1
	r.mu.Unlock()

	c.mu.Lock()
	ignored(c)
	c.mu.Unlock()
}

func deferredUnlock(c *counter) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.n = 1
}

func deferredLiteral(c *counter) {
	c.mu.Lock()
	defer func() {
		c.n = 2
		c.mu.Unlock()
	}()
	c.n = 1
}

func deferredRelease(c *counter) {
	acquires(c)
	defer releases(c)

	c.n = 1
}

func inlineLiteral(c *counter) {
	c.mu.Lock()
	func() {
		c.n = 1
		func() {
			c.n = 2
		}()
	}()
	c.mu.Unlock()
}

func inlineLocks(c *counter) {
	func() {
		c.mu.Lock()
	}()
	c.n = 1
	func() {
		c.mu.Unlock()
	}()
}

func inlineParameter(c, other *counter) {
	c.mu.Lock()
	func(p *counter) {
		c.n = 1
		p.mu.Lock()
	}(other)
	other.n = 2
	other.mu.Unlock()
	c.mu.Unlock()
}

func inlineConsistentBranches(c *counter, cond bool) {
	func() {
		if cond {
			c.mu.Lock()
			return
		}

		c.mu.Lock()
	}()
	c.n = 1
	c.mu.Unlock()
}

func escapedLiteralLocks(c *counter) {
	use(func() {
		c.mu.Lock()
		c.n = 1
		c.mu.Unlock()
	})
}

func capturedPointer(c, other *counter) {
	p := c
	c.mu.Lock()
	defer func(arg *counter) {
		arg.mu.Unlock()
	}(p)
	func() {
		p = other
	}()
}

func reassignedPointer(c, other *counter) {
	p := c
	saved := p
	func() {
		p = other
	}()
	c.mu.Lock()
	saved.n = 1
	c.mu.Unlock()
	other.mu.Lock()
	p.n = 2
	other.mu.Unlock()
}

func conditionalStore(c, other *counter, cond bool) {
	p := c
	if cond {
		func() {
			p = other
		}()
	}

	p.mu.Lock()
	p.n = 1
	p.mu.Unlock()
}

type node struct {
	mu   sync.Mutex
	next *node
	// +checklocks:mu
	n int
}

func walk(head *node) {
	cur := head
	advance := func() { cur = cur.next }

	for cur != nil {
		cur.mu.Lock()
		cur.n++
		cur.mu.Unlock()
		advance()
	}
}

func twoSites(a, b *counter) {
	var p *counter

	set := func(x *counter) { p = x }

	set(a)
	a.mu.Lock()
	p.n = 1
	a.mu.Unlock()
	set(b)
	b.mu.Lock()
	p.n = 2
	b.mu.Unlock()
}

func neverReturns(c *counter) {
	c.mu.Lock()
	c.n = 1
	func() {
		panic("x")
	}()
	c.mu.Unlock()
}

func indexed(cs []*counter) {
	i := 0
	cs[i].mu.Lock()
	cs[i].n = 1
	cs[i].mu.Unlock()
	cs[1].mu.Lock()
	cs[1].n = 2
	cs[1].mu.Unlock()
}

func fresh() {
	c := new(counter)
	c.n = 1

	var local counter
	local.n = 2
	local.mu.Lock()
	local.mu.Unlock()
}

func unannotated(c *counter) {
	var mu sync.Mutex
	mu.Lock()
	mu.Unlock()
	use(c)
}

type box[T any] struct {
	mu sync.Mutex
	// +checklocks:mu
	value T
}

func generic[T any](b *box[T], v T) {
	b.mu.Lock()
	b.value = v
	b.mu.Unlock()
}

func recovers(c *counter) {
	defer func() {
		if recover() != nil {
			use(c)
		}
	}()

	c.mu.Lock()
	c.n = 1
	c.mu.Unlock()
}
