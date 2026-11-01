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

// Package guardedlib declares guards and contracts that package guardedclient uses through
// facts, including private global locks that callers lock through functions.
package guardedlib

import "sync"

var (
	// Mu guards Count.
	Mu sync.Mutex
	// +checklocks:Mu
	Count int
)

var privateMu sync.Mutex

// +checklocks:privateMu
var Private int

// Counter is a guarded counter.
type Counter struct {
	Mu sync.Mutex
	// +checklocks:Mu
	N int
}

// Box is a generic guarded box.
type Box[T any] struct {
	Mu sync.Mutex
	// +checklocks:Mu
	Value T
}

// Inc needs the lock.
//
// +checklocks:c.Mu
func (c *Counter) Inc() { c.N++ }

// LockPrivate locks the private lock for its caller.
//
// +checklocksacquire:privateMu
func LockPrivate() { privateMu.Lock() }

// UnlockPrivate unlocks the private lock its caller holds.
//
// +checklocksrelease:privateMu
func UnlockPrivate() { privateMu.Unlock() }

// RequirePrivate needs the private lock.
//
// +checklocks:privateMu
func RequirePrivate() {}

// ExcludePrivate must not be called with the private lock held.
//
// +checklocksexclude:privateMu
func ExcludePrivate() {}
