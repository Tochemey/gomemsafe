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

// Package guardedclient uses package guardedlib, whose guards and contracts it learns from
// facts. A private global lock of guardedlib is named by its package, so the same-named lock
// here is another lock.
package guardedclient

import (
	"sync"

	"guardedlib"
)

var privateMu sync.Mutex

type wrapper struct {
	guardedlib.Counter
	// +checklocks:Counter.Mu
	extra int
}

func use(...any) {}

func globals() {
	guardedlib.Count = 1 // want `MS403: guardedlib.Count written here without holding guardedlib.Mu`
	guardedlib.Mu.Lock()
	guardedlib.Count = 2
	guardedlib.Mu.Unlock()
}

func method(c *guardedlib.Counter) {
	c.Inc() // want `MS403: Inc called here without holding c.Mu, which it requires`
	c.Mu.Lock()
	c.Inc()
	c.N = 1
	c.Mu.Unlock()
	c.N = 2 // want `MS403: N written here without holding c.Mu`
}

func private() {
	guardedlib.RequirePrivate() // want `MS403: RequirePrivate called here without holding guardedlib.privateMu, which it requires`
	guardedlib.Private = 1 // want `MS403: guardedlib.Private written here without holding guardedlib.privateMu`
	guardedlib.ExcludePrivate()
	guardedlib.LockPrivate()
	guardedlib.RequirePrivate()
	guardedlib.Private = 2
	guardedlib.ExcludePrivate() // want `MS403: ExcludePrivate called here while holding guardedlib.privateMu, which it must not be`
	guardedlib.UnlockPrivate()
	guardedlib.RequirePrivate() // want `MS403: RequirePrivate called here without holding guardedlib.privateMu, which it requires`

	privateMu.Lock()
	guardedlib.RequirePrivate() // want `MS403: RequirePrivate called here without holding guardedlib.privateMu, which it requires`
	privateMu.Unlock()
}

func generic(b *guardedlib.Box[int]) {
	b.Value = 1 // want `MS403: Value written here without holding b.Mu`
	b.Mu.Lock()
	b.Value = 2
	b.Mu.Unlock()
}

func embedded(w *wrapper) {
	w.extra = 1 // want `MS403: extra written here without holding w.Counter.Mu`
	w.N = 1 // want `MS403: N written here without holding w.Counter.Mu`
	w.Mu.Lock()
	w.extra = 2
	w.N = 2
	w.Mu.Unlock()
}
