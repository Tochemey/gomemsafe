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

import (
	"go/token"
	"go/types"
	"testing"
)

func TestSynchronizesKnowsTheAcquiringCalls(t *testing.T) {
	for _, tc := range []struct{ typ, name string }{
		{"WaitGroup", "Wait"},
		{"Mutex", "Lock"},
		{"Mutex", "TryLock"},
		{"RWMutex", "Lock"},
		{"RWMutex", "RLock"},
		{"RWMutex", "TryLock"},
		{"RWMutex", "TryRLock"},
		{"Cond", "Wait"},
		{"Once", "Do"},
		{"Map", "Load"},
		{"Map", "LoadAndDelete"},
		{"Map", "LoadOrStore"},
		{"Map", "Swap"},
		{"Map", "CompareAndSwap"},
		{"Map", "CompareAndDelete"},
		{"Pool", "Get"},
	} {
		if !Synchronizes(method(t, "sync", tc.typ, tc.name)) {
			t.Errorf("Synchronizes((*sync.%s).%s) = false, want true", tc.typ, tc.name)
		}
	}
}

func TestSynchronizesKnowsEveryAtomicOperation(t *testing.T) {
	if !Synchronizes(importPackage(t, "sync/atomic").Scope().Lookup("AddInt64").(*types.Func)) {
		t.Error("Synchronizes(atomic.AddInt64) = false, want true")
	}

	if !Synchronizes(method(t, "sync/atomic", "Bool", "Store")) {
		t.Error("Synchronizes((*atomic.Bool).Store) = false, want true")
	}
}

func TestSynchronizesIgnoresReleasesAndOtherCalls(t *testing.T) {
	for _, tc := range []struct{ typ, name string }{
		{"WaitGroup", "Done"},
		{"WaitGroup", "Add"},
		{"Mutex", "Unlock"},
		{"RWMutex", "RUnlock"},
		{"Cond", "Signal"},
		{"Map", "Store"},
		{"Map", "Range"},
		{"Pool", "Put"},
	} {
		if Synchronizes(method(t, "sync", tc.typ, tc.name)) {
			t.Errorf("Synchronizes((*sync.%s).%s) = true, want false", tc.typ, tc.name)
		}
	}

	if Synchronizes(nil) {
		t.Error("Synchronizes(nil) = true")
	}

	if Synchronizes(types.NewFunc(token.NoPos, nil, "local", types.NewSignatureType(nil, nil, nil, nil, nil, false))) {
		t.Error("Synchronizes found an effect for a function without a package")
	}
}

func TestSyncKnownCoversTheSyncPackagesOnly(t *testing.T) {
	if !SyncKnown(method(t, "sync", "Mutex", "Unlock")) || !SyncKnown(method(t, "sync/atomic", "Int32", "Load")) {
		t.Error("SyncKnown is false for a function of sync or sync/atomic")
	}

	if SyncKnown(method(t, "bytes", "Buffer", "Write")) || SyncKnown(nil) {
		t.Error("SyncKnown is true outside sync and sync/atomic")
	}
}
