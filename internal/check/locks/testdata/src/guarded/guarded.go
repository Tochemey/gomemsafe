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

// Package guarded breaks the lock contracts its checklocks annotations declare. MS403 must
// report each one.
package guarded

import "sync"

var globalMu sync.Mutex

// +checklocks:globalMu
var total int

var registry struct {
	mu sync.RWMutex
	// +checklocks:mu
	entries map[string]int
}

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
}

type nested struct {
	inner counter
	ptr   *counter
	// +checklocks:inner.mu
	viaValue int
	// +checklocks:ptr.mu
	viaPointer int
}

type embedded struct {
	sync.Mutex
	// +checklocks:Mutex
	n int
}

type withLocker struct {
	l sync.Locker
	// +checklocks:l
	n int
}

func use(...any) {}

func writeUnlocked(c *counter) {
	c.n = 1 // want `MS403: n written here without holding c.mu`
}

func readUnlocked(c *counter) int {
	return c.n // want `MS403: n read here without holding c.mu`
}

func writeUnderReadLock(c *counter) {
	c.rw.RLock()
	c.readMostly = 1 // want `MS403: readMostly written here without holding c.rw`
	c.rw.RUnlock()
}

func onlyOneOfTwo(c *counter) {
	c.mu.Lock()
	c.both = 1 // want `MS403: both written here without holding c.rw`
	c.mu.Unlock()
}

func wrongReceiver(a, b *counter) {
	a.mu.Lock()
	b.n = 1 // want `MS403: n written here without holding b.mu`
	a.mu.Unlock()
}

func nestedValue(x *nested) {
	x.viaValue = 1 // want `MS403: viaValue written here without holding x.inner.mu`
	x.ptr.mu.Lock()
	x.viaPointer = 1
	x.viaValue = 2 // want `MS403: viaValue written here without holding x.inner.mu`
	x.ptr.mu.Unlock()
}

func embeddedMutex(e *embedded) {
	e.n = 1 // want `MS403: n written here without holding e.Mutex`
	e.Lock()
	e.n = 2
	e.Unlock()
}

func locker(w *withLocker) {
	w.n = 1 // want `MS403: n written here without holding w.l`
	w.l.Lock()
	w.n = 2
	w.l.Unlock()
}

func globals() {
	total++ // want `MS403: total read here without holding globalMu` `MS403: total written here without holding globalMu`
	registry.entries["a"] = 1 // want `MS403: entries read here without holding registry.mu`
	use(&total) // want `MS403: total read here without holding globalMu`
}

func addressPassed(c *counter) {
	use(&c.n) // want `MS403: n read here without holding c.mu`
}

func branches(c *counter, cond bool) { // want `MS403: branches returns holding c.mu on some paths and not on others` `MS403: branches returns here holding c.mu`
	if cond {
		c.mu.Lock()
	}

	c.n = 1 // want `MS403: n written here without holding c.mu`
}

func loopLocks(cs []*counter) {
	for _, c := range cs {
		c.mu.Lock()
	}

	for _, c := range cs {
		c.n = 1 // want `MS403: n written here without holding cs\[...\].mu`
	}
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
	return result // want `MS403: acquiresResult returns here without holding result.mu, which it must leave held`
}

// +checklocksacquire:second.mu
func acquiresSecond() (first int, second *counter) {
	second = new(counter)
	second.mu.Lock()

	return 1, second
}

func calls(c *counter) {
	requires(c) // want `MS403: requires called here without holding c.mu, which it requires`
	use(requiresRead(c)) // want `MS403: requiresRead called here without holding c.rw, which it requires`
	c.mu.Lock()
	excludes(c) // want `MS403: excludes called here while holding c.mu, which it must not be`
	acquires(c) // want `MS403: acquires called here to acquire c.mu, which is already held`
	c.mu.Unlock()
	releases(c) // want `MS403: releases called here to release c.mu, which is not held`
	c.n = 1 // want `MS403: n written here without holding c.mu`
	_, second := acquiresSecond()
	second.mu.Unlock()
	second.mu.Unlock() // want `MS403: acquiresSecond\(\)\[1\].mu unlocked here while not held`
}

func doubleLock(c *counter) {
	c.mu.Lock()
	c.mu.Lock() // want `MS403: c.mu locked here while already held`
	c.mu.Unlock()
	c.rw.RLock()
	c.rw.Unlock() // want `MS403: c.rw unlocked here while not held`
}

func leaks(c *counter) { // want `MS403: leaks returns here holding c.mu`
	c.mu.Lock()
}

func leaksExplicit(c *counter) int {
	c.mu.Lock()
	return c.n // want `MS403: leaksExplicit returns here holding c.mu`
}

// +checklocks:c.mu
func releasesRequired(c *counter) { // want `MS403: releasesRequired returns here without holding c.mu, which it must leave held`
	c.mu.Unlock()
	c.n = 1 // want `MS403: n written here without holding c.mu`
}

func deferredAccess(c *counter) {
	c.mu.Lock()
	defer func() {
		c.n = 1 // want `MS403: n written here without holding c.mu`
	}()
	c.mu.Unlock()
}

func inlineLiteral(c *counter) {
	func() {
		c.mu.Lock()
	}()
	c.n = 1
	func() {
		c.mu.Unlock()
	}()
	c.n = 2 // want `MS403: n written here without holding c.mu`
}

func inlineModes(c *counter, cond bool) {
	func() { // want `MS403: inlineModes\$1 returns holding c.rw on some paths and not on others`
		if cond {
			c.rw.Lock()
		} else {
			c.rw.RLock()
		}
	}()
}

func inlineLeak(c *counter, cond bool) {
	func() { // want `MS403: inlineLeak\$1 returns holding c.mu on some paths and not on others`
		if cond {
			c.mu.Lock()
		}
	}()
}

func escapedLiteral(c *counter) {
	c.mu.Lock()
	use(func() {
		c.n = 1 // want `MS403: n written here without holding c.mu`
	})
	go func() {
		c.n = 2 // want `MS403: n written here without holding c.mu`
	}()
	c.mu.Unlock()
}

func literalParameter(c *counter) {
	f := func(p *counter) {
		p.n = 1 // want `MS403: n written here without holding p.mu`
	}
	c.mu.Lock()
	f(c)
	use(f)
	c.mu.Unlock()
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
	p.n = 1 // want `MS403: n written here without holding other.mu`
}

func twoSites(a, b *counter) {
	var p *counter

	set := func(x *counter) { p = x }

	set(a)
	set(b)
	a.mu.Lock()
	p.n = 1 // want `MS403: n written here without holding b.mu`
	a.mu.Unlock()
}

func unknownContents(c, other *counter, cond bool) {
	p := c
	if cond {
		func() {
			p = other
		}()
	}

	c.mu.Lock()
	p.n = 1 // want `MS403: n written here without holding p.mu`
	c.mu.Unlock()
}

func indexed(cs []*counter) {
	i := 0
	cs[i].mu.Lock()
	cs[i].n = 1
	cs[1].n = 2 // want `MS403: n written here without holding cs\[1\].mu`
	cs[i].mu.Unlock()
}

type box[T any] struct {
	mu sync.Mutex
	// +checklocks:mu
	value T
}

func generic[T any](b *box[T], v T) {
	b.value = v // want `MS403: value written here without holding b.mu`
}

func fresh() *counter {
	c := new(counter)
	c.n = 1

	var local counter
	local.n = 2

	return c
}
