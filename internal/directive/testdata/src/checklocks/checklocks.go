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

// Package checklocks holds well-formed and malformed checklocks annotations.
package checklocks

import "sync"

var globalMu sync.Mutex

var globalBox struct {
	mu sync.RWMutex
	// +checklocks:mu
	count int
}

// +checklocks:globalMu
var first, _, second int

var trailing int // +checklocks:globalMu

var (
	// +checklocks:globalMu
	grouped int
	// +checklocks:globalBox.mu
	nested int
)

// +checklocks:globalMu
var (
	headerFirst int
	headerBoth  int // +checklocks:globalBox.mu
)

// +checklocks:missing
var unresolved int // want `MS000: \+checklocks:missing does not resolve to a global lock, for unresolved`

// +checklocks:first
var notALock int // want `MS000: \+checklocks:first names first, which is not a Mutex, an RWMutex, or a sync.Locker`

// +checkatomic
var atomicGlobal int32 // want `MS000: \+checkatomic is not supported yet by this version of gomemsafe`

type inner struct {
	mu sync.Mutex
}

type embedded struct {
	sync.RWMutex
}

type guarded struct {
	mu     sync.Mutex
	rw     sync.RWMutex
	locker sync.Locker
	ptr    *sync.Mutex
	in     inner
	inPtr  *inner
	embedded

	// +checklocks:mu
	byDoc int

	// +checklocks:mu
	pair1, pair2 int

	byLine int // +checklocks:mu

	// +checklocks:mu
	// +checklocks:rw
	twoLocks int

	// +checklocks:in.mu
	viaValue int
	// +checklocks:inPtr.mu
	viaPointer int
	// +checklocks:RWMutex
	viaEmbedded int
	// +checklocks:locker
	viaLocker int
	// +checklocks:ptr
	viaPointerLock int
	// +checklocks:globalMu
	viaGlobal int
	// +checklocks:globalBox.mu
	viaGlobalField int

	// +checklocks:mu
	// +checklocks:mu
	twice int // want `MS000: \+checklocks:mu is declared twice on twice`

	// +checklocks:nothing
	unresolved int // want `MS000: \+checklocks:nothing does not resolve to a field of the struct declaring unresolved, or to a global`

	// +checklocks:byDoc
	notALock int // want `MS000: \+checklocks:byDoc names byDoc, which is not a Mutex, an RWMutex, or a sync.Locker`

	// +checklocksread:rw
	readOnField int // want `MS000: \+checklocksread is not valid on readOnField; a field or a variable takes \+checklocks:<lock>`

	// +checklocksignore: inference is silenced, which gomemsafe does not do anyway
	ignored int

	marked int // +checklocksfail

	// +checklocksreadany
	readAny int // want `MS000: \+checklocksreadany is not supported yet by this version of gomemsafe`
}

type anonymous struct {
	box struct {
		mu sync.Mutex
		// +checklocks:mu
		value int
	}
}

type generic[T any] struct {
	mu sync.Mutex
	// +checklocks:mu
	value T
}

// +checklocksalias:in.mu=mu
type aliased struct { // want `MS000: \+checklocksalias is not supported yet by this version of gomemsafe`
	mu sync.Mutex
	in inner
}

// +checklocks:mu
type misplaced struct { // want `MS000: \+checklocks must be in the doc comment of a field, a variable, or a function`
	mu sync.Mutex
}

// +checklocksignore
type ignoredType struct {
	mu sync.Mutex
}

// +checklocks:g.mu
func (g *guarded) requires() {}

// +checklocksread:g.rw
func (g *guarded) requiresRead() {}

// +checklocks:g.mu
// +checklocks:other.in.mu
// +checklocksread:other.rw
// +checklocksexclude:g.rw
// +checklocksexclude:globalMu
func (g *guarded) mixed(n int, other *guarded) {}

// +checklocksacquire:g.mu
func (g *guarded) acquires() {}

// +checklocksrelease:g.mu
func (g *guarded) releases() {}

// +checklocksacquire:result.mu
// +checklocksacquire:second.in.mu
func acquiresResults() (result *guarded, second *guarded) { return nil, nil }

// +checklocks:globalMu
// +checklocks:globalBox.mu
func requiresGlobals() {}

// +checklocks:g.mu
// +checklocksignore
func ignored(g *guarded) {}

// +checklocksignore
func onlyIgnored() {}

// +checklocks:absent.mu
func unresolvedParam() {} // want `MS000: \+checklocks:absent.mu does not resolve to a parameter, a result, or a global of unresolvedParam`

// +checklocks:result.mu
func requiresResult() (result *guarded) { return nil } // want `MS000: \+checklocks:result.mu does not resolve to a parameter, a result, or a global of requiresResult`

// +checklocksread:g.mu
func readsMutex(g *guarded) {} // want `MS000: \+checklocksread:g.mu names mu, which is not an RWMutex`

// +checklocks:g.byDoc
func requiresNonLock(g *guarded) {} // want `MS000: g.byDoc names byDoc, which is not a Mutex, an RWMutex, or a sync.Locker`

// +checklocks:g.mu
// +checklocksacquire:g.mu
func twiceOnFunc(g *guarded) {} // want `MS000: \+checklocks:g.mu is declared twice on twiceOnFunc`

// +checklocks:g.mu
// +checklocksexclude:g.mu
func requiredAndExcluded(g *guarded) {} // want `MS000: g.mu is both required and excluded by requiredAndExcluded`

// +checklocksreadany
func readAnyOnFunc() {} // want `MS000: \+checklocksreadany is not supported yet by this version of gomemsafe`

// +checkatomic
func atomicOnFunc() {} // want `MS000: \+checkatomic is not supported yet by this version of gomemsafe`

// +checklocksfail
func failOnFunc() {}

// +checklocksacquire:g.mu
// +checklocksacquire:g.mu
func acquiresTwice(g *guarded) {} // want `MS000: \+checklocks:g.mu is declared twice on acquiresTwice`

// +checklocksexclude:g.mu
// +checklocks:g.mu
func excludedAndRequired(g *guarded) {} // want `MS000: g.mu is both required and excluded by excludedAndRequired`

// +checklocksexclude:g.mu
// +checklocksexclude:g.mu
func excludesTwice(g *guarded) {} // want `MS000: \+checklocks:g.mu is declared twice on excludesTwice`

func lines(g *guarded) {
	type local struct {
		mu sync.Mutex
		// +checklocks:mu
		value int
	}

	g.byDoc = 1 // +checklocksforce
	g.byDoc = 2 // +checklocksignore
	g.byDoc = 3 // +checklocksfail
	/* want `MS000: \+checklocks must be in the doc comment of a field, a variable, or a function` */ g.byDoc = 4 // +checklocks:mu
}
