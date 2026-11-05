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

// Package contracts calls and implements interface methods with contracts. Every call is checked
// as if the contract held, and every implementation that breaks one is reported.
package contracts

import (
	"sync"

	"contractlib"
)

// Frame is a value with no ownership of its own: a consuming parameter moves any value.
type Frame struct{ data []byte }

// Writer takes what it is given.
type Writer interface {
	//memsafe:consumes f
	Write(f *Frame)
}

// GenericWriter is a generic interface with a contract.
type GenericWriter[T any] interface {
	//memsafe:consumes v
	Write(v *T)
}

// Peeker keeps nothing of what it is given.
type Peeker interface {
	//memsafe:borrows p
	Peek(p []byte) int
}

// Pair borrows one parameter, and says nothing of the other.
type Pair interface {
	//memsafe:borrows q
	Two(p, q []byte)
}

// Echo must not return what it borrows either.
type Echo interface {
	//memsafe:borrows p
	Echo(p []byte) []byte
}

// Looker must not take back what it borrows.
type Looker interface {
	//memsafe:borrows f
	Look(f *Frame)
}

// Viewer returns a view into its first parameter.
type Viewer interface {
	//memsafe:viewof a
	View(a, b *Frame) []byte
}

// Resetter ends the views of its first parameter.
type Resetter interface {
	//memsafe:invalidates a
	Reset(a, b *Frame)
}

var (
	pool   sync.Pool
	global []byte
)

func use(...any) {}

func writes(w Writer, f *Frame) {
	w.Write(f)
	use(f) // want `MS501: f used after it was moved`
}

func writesInGoroutine(w Writer, f *Frame) {
	go w.Write(f)
	use(f) // want `MS501: f used after it was moved`
}

func writesImported(s contractlib.Sink, f *contractlib.Frame) {
	s.Write(f)
	use(f) // want `MS501: f used after it was moved`
}

func writesGeneric[T any](w GenericWriter[T], v *T) {
	w.Write(v)
	use(v) // want `MS501: v used after it was moved`
}

//memsafe:borrows p
func passesUndeclared(x Pair, p []byte) {
	x.Two(p, nil) // want `MS502: p escapes passesUndeclared, which borrows it: passed to x.Two, which may keep it`
}

//memsafe:borrows f
func passesConsumed(w Writer, f *Frame) {
	w.Write(f) // want `MS502: f escapes passesConsumed, which borrows it: passed to the consuming parameter f of w.Write`
}

// Lender's result is a view into what it borrows.
type Lender interface {
	//memsafe:borrows p
	//memsafe:viewof p
	Lend(p []byte) []byte
}

//memsafe:borrows p
func returnsLent(x Lender, p []byte) []byte {
	return x.Lend(p) // want `MS502: .* escapes returnsLent, which borrows p: returned`
}

//memsafe:borrows x
func passesReceiver(x Peeker, p []byte) {
	x.Peek(p) // want `MS502: x escapes passesReceiver, which borrows it: passed to x.Peek, which may keep it`
}

type keeps struct{ kept []byte }

func (k *keeps) Peek(p []byte) int { // want `MS504: keeps.Peek keeps p, which Peeker.Peek borrows`
	k.kept = p
	return 0
}

// outer implements Peeker through the method of keeps, which is reported once.
type outer struct{ keeps }

type valueKeeper struct{}

func (valueKeeper) Peek(p []byte) int { // want `MS504: valueKeeper.Peek keeps p, which Peeker.Peek borrows`
	global = p
	return 0
}

type genericKeeper[T any] struct{ kept []byte }

func (g *genericKeeper[T]) Peek(p []byte) int { // want `MS504: genericKeeper\[T\].Peek keeps p, which Peeker.Peek borrows`
	g.kept = p
	return 0
}

type consumes struct{}

//memsafe:consumes p
func (consumes) Peek(p []byte) int { return len(p) } // want `MS504: consumes.Peek consumes p, which Peeker.Peek does not consume`

type consumesUndeclared struct{}

//memsafe:consumes p
func (consumesUndeclared) Two(p, q []byte) { use(len(p), len(q)) } // want `MS504: consumesUndeclared.Two consumes p, which Pair.Two does not consume`

type returns struct{}

func (returns) Echo(p []byte) []byte { return p } // want `MS504: returns.Echo keeps p, which Echo.Echo borrows`

type takes struct{}

func (takes) Look(f *Frame) { pool.Put(f) } // want `MS504: takes.Look takes back f, which Looker.Look does not consume`

type viewsOther struct{}

//memsafe:viewof b
func (viewsOther) View(a, b *Frame) []byte { return b.data } // want `MS504: viewsOther.View returns a view into b, which Viewer.View does not declare`

type endsOther struct{}

//memsafe:invalidates b
func (endsOther) Reset(a, b *Frame) {} // want `MS504: endsOther.Reset ends the views of b, which Resetter.Reset does not declare`

// ConsumingPeeker's contract consumes what Peeker's borrows.
type ConsumingPeeker interface {
	//memsafe:consumes p
	Peek(p []byte) int // want `MS504: ConsumingPeeker.Peek consumes p, which Peeker.Peek does not consume`
}

// embedsConsuming implements Peeker through an interface whose contract breaks Peeker's.
type embedsConsuming struct{ ConsumingPeeker }

// embedsImported is reported at the type, as its method is declared in another package.
type embedsImported struct{ *contractlib.Keeper } // want `MS504: contractlib.Keeper.Peek keeps p, which contractlib.Sink.Peek borrows` `MS504: contractlib.Keeper.Peek keeps p, which Peeker.Peek borrows`
