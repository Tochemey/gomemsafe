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

package effect

import "go/types"

// Full names of the synchronising callees, as types.Func.FullName prints them, and the packages
// whose documentation says which calls synchronise.
const (
	condWait          = "(*sync.Cond).Wait"
	mapCompareAndDel  = "(*sync.Map).CompareAndDelete"
	mapCompareAndSwap = "(*sync.Map).CompareAndSwap"
	mapLoad           = "(*sync.Map).Load"
	mapLoadAndDelete  = "(*sync.Map).LoadAndDelete"
	mapLoadOrStore    = "(*sync.Map).LoadOrStore"
	mapSwap           = "(*sync.Map).Swap"
	mutexLock         = "(*sync.Mutex).Lock"
	mutexTryLock      = "(*sync.Mutex).TryLock"
	onceDo            = "(*sync.Once).Do"
	poolGet           = "(*sync.Pool).Get"
	rwMutexLock       = "(*sync.RWMutex).Lock"
	rwMutexRLock      = "(*sync.RWMutex).RLock"
	rwMutexTryLock    = "(*sync.RWMutex).TryLock"
	rwMutexTryRLock   = "(*sync.RWMutex).TryRLock"
	waitGroupWait     = "(*sync.WaitGroup).Wait" //nolint:gosec // G101 matches "pW" in the name; the value is a method name, not a credential

	atomicPath = "sync/atomic"
	syncPath   = "sync"
)

// synchronizing lists the callees whose return the Go memory model orders after an operation
// of another goroutine, each with the sentence of its documentation that says so. Keyed by
// types.Func.FullName.
var synchronizing = map[string]bool{
	// "a call to Done "synchronizes before" the return of any Wait call that it unblocks."
	waitGroupWait: true,
	// "the n'th call to Mutex.Unlock "synchronizes before" the m'th call to Mutex.Lock for any
	// n < m. A successful call to Mutex.TryLock is equivalent to a call to Lock."
	mutexLock:    true,
	mutexTryLock: true,
	// "For any call to RLock, there exists an n such that the n'th call to Unlock "synchronizes
	// before" that call to RLock", and RWMutex's Lock and TryLock are as for Mutex.
	rwMutexLock:     true,
	rwMutexRLock:    true,
	rwMutexTryLock:  true,
	rwMutexTryRLock: true,
	// "Cond.Broadcast or Cond.Signal "synchronizes before" any Wait call that it unblocks."
	condWait: true,
	// "the return from f "synchronizes before" the return from any call of once.Do(f)."
	onceDo: true,
	// "a write operation "synchronizes before" any read operation that observes the effect of
	// the write", where "Map.Load, Map.LoadAndDelete, Map.LoadOrStore, Map.Swap,
	// Map.CompareAndSwap, and Map.CompareAndDelete are read operations".
	mapLoad:           true,
	mapLoadAndDelete:  true,
	mapLoadOrStore:    true,
	mapSwap:           true,
	mapCompareAndSwap: true,
	mapCompareAndDel:  true,
	// "a call to Put(x) "synchronizes before" a call to Get returning that same value x."
	poolGet: true,
}

// Synchronizes reports whether a call of fn can complete a happens-before edge from another
// goroutine: the Go memory model says an operation of that goroutine synchronizes before the
// call returns, so its earlier writes are visible after the call. It holds for the calls in
// synchronizing and for every operation of package sync/atomic: "if the effect of an atomic
// operation A is observed by atomic operation B, then A "synchronizes before" B". An atomic
// store observes nothing, but counting it costs only a missed race, never a false one.
// Releasing calls, such as Mutex.Unlock or WaitGroup.Done, complete no edge in their caller.
func Synchronizes(fn *types.Func) bool {
	if !SyncKnown(fn) {
		return false
	}

	return fn.Pkg().Path() == atomicPath || synchronizing[fn.Origin().FullName()]
}

// SyncKnown reports whether fn synchronises is known rather than derived from its code:
// fn belongs to package sync or sync/atomic, whose documentation says which calls synchronise.
// Their code synchronises in more calls than that: Mutex.Unlock and WaitGroup.Done run atomic
// operations, yet complete no edge in their caller.
func SyncKnown(fn *types.Func) bool {
	if fn == nil || fn.Pkg() == nil {
		return false
	}

	path := fn.Pkg().Path()

	return path == syncPath || path == atomicPath
}
