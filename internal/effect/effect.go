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

// Package effect records the known effects of library calls on their arguments: the effects
// that cannot be derived from the callee's code, such as sync.Pool.Put handing its argument
// back to the pool. Known effects take precedence over computed summaries (package summary),
// which derive the effect of every function that calls one of these.
//
// To teach gomemsafe another effect, add the callee's full name (as types.Func.FullName prints
// it) and its Invalidation to known. Argument indexes count the receiver as 0, matching the
// argument order of an SSA call. Each new entry needs a test in effect_test.go and a fixture
// under internal/check/lifetime/testdata.
package effect

import "go/types"

// Full names of the callees with a known effect, as types.Func.FullName prints them.
const (
	syncPoolPut = "(*sync.Pool).Put"
)

// Descriptions of invalidations, as diagnostics word them.
const (
	// ReturnedToPool describes a value handed back to a sync.Pool.
	ReturnedToPool = "returned to sync.Pool"
)

// known is keyed by types.Func.FullName.
var known = map[string]Invalidation{
	syncPoolPut: {Args: 1 << 1, Description: ReturnedToPool},
}

// ArgSet is a set of call-argument indexes. For a static method call the receiver is index 0,
// matching the argument order of an SSA call.
type ArgSet uint64

// Invalidation is what a call does to some of its arguments: it takes them back, so the
// caller must not use them after the call returns.
type Invalidation struct {
	// Args lists the invalidated call arguments.
	Args ArgSet
	// Description words the invalidation for diagnostics, e.g. "returned to sync.Pool".
	Description string
}

// Of returns the known invalidation of fn, if it has one.
func Of(fn *types.Func) (Invalidation, bool) {
	if fn == nil {
		return Invalidation{}, false
	}

	inv, ok := known[fn.Origin().FullName()]

	return inv, ok
}

// Has reports whether index i is in the set.
func (s ArgSet) Has(i int) bool { return i >= 0 && i < 64 && s&(1<<uint(i)) != 0 }
