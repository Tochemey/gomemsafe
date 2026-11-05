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

// Package contractsfalsepos calls interface methods within their contracts, and implements them
// with methods that do no more than the contracts declare. None of it is reported.
package contractsfalsepos

// Frame is a value.
type Frame struct{ data []byte }

// Writer takes what it is given.
type Writer interface {
	//memsafe:consumes f
	Write(f *Frame)
}

// GenericKeeper is a generic interface: no type implements it as written.
type GenericKeeper[T any] interface {
	//memsafe:borrows p
	Keep(p []T)
}

// Peeker keeps nothing of what it is given.
type Peeker interface {
	//memsafe:borrows p
	Peek(p []byte) int
}

// Viewer returns a view into its parameter.
type Viewer interface {
	//memsafe:viewof f
	View(f *Frame) []byte
}

// Resetter ends the views of its parameter.
type Resetter interface {
	//memsafe:invalidates f
	Reset(f *Frame)
}

// Plain has no contract, so it rules nothing out for its implementations.
type Plain interface {
	Plain(f *Frame)
}

// Mixed has a contract on one method only.
type Mixed interface {
	//memsafe:borrows p
	Annotated(p []byte)

	Unannotated(p []byte)
}

var global []byte

func use(...any) {}

func usedBefore(w Writer, f *Frame) {
	use(f)
	w.Write(f)
}

func assignedAgain(w Writer, f *Frame) {
	w.Write(f)

	f = new(Frame)
	use(f)
}

// methodValue is a function value: its contract is not followed.
func methodValue(w Writer, f *Frame) {
	write := w.Write
	write(f)
	use(f)
}

func notConsumed(x Peeker, f *Frame) {
	x.Peek(f.data)
	use(f)
}

//memsafe:borrows p
func forwards(x Peeker, p []byte) int { return x.Peek(p) }

type keeps struct{ kept *Frame }

// Write may keep what it consumes.
func (k *keeps) Write(f *Frame) { k.kept = f }

type declaresConsumes struct{}

//memsafe:consumes f
func (declaresConsumes) Write(f *Frame) { use(f) }

type ignores struct{}

func (ignores) Write(*Frame) {}

type reads struct{}

func (reads) Peek(p []byte) int { return len(p) }

type declaresBorrows struct{}

//memsafe:borrows p
func (declaresBorrows) Peek(p []byte) int { return len(p) }

// wraps answers through the interface it embeds.
type wraps struct{ Peeker }

// Lender declares both a borrow and a view of one parameter.
type Lender interface {
	//memsafe:borrows p
	//memsafe:viewof p
	Lend(p []byte) []byte
}

// wrapsLender implements Lender through Lender itself, which breaks nothing of its own contract.
type wrapsLender struct{ Lender }

// aliased names a type that is checked under its own name.
type aliased = reads

// notPeeker keeps p, and implements nothing: its method has another signature.
type notPeeker struct{}

func (notPeeker) Peek(p []byte) { global = p }

// genericImpl keeps p: a generic interface's implementations are not checked.
type genericImpl struct{}

func (genericImpl) Keep(p []byte) { global = p }

type declaresView struct{}

//memsafe:viewof f
func (declaresView) View(f *Frame) []byte { return f.data }

// PairViewer returns a view into its first parameter.
type PairViewer interface {
	//memsafe:viewof a
	ViewPair(a, b *Frame) []byte
}

type viewsFirst struct{}

//memsafe:viewof a
func (viewsFirst) ViewPair(a, b *Frame) []byte { return a.data[:len(b.data)] }

// TakingViewer consumes its parameter, and declares no view.
type TakingViewer interface {
	//memsafe:consumes f
	Take(f *Frame) []byte
}

type viewsConsumed struct{}

// Take's caller holds no more claim on f, so nothing it does can end the view.
//
//memsafe:viewof f
func (viewsConsumed) Take(f *Frame) []byte { return f.data }

type copies struct{}

func (copies) View(f *Frame) []byte { return append([]byte(nil), f.data...) }

type declaresEnds struct{}

//memsafe:invalidates f
func (declaresEnds) Reset(f *Frame) { f.data = nil }

type endsNothing struct{}

func (endsNothing) Reset(*Frame) {}

// consumesReceiver takes its receiver, which a contract cannot name, so none rules it out.
type consumesReceiver struct{ kept []byte }

//memsafe:consumes c
func (c consumesReceiver) Peek(p []byte) int { return len(c.kept) + len(p) }

type plainConsumes struct{}

//memsafe:consumes f
func (plainConsumes) Plain(f *Frame) { use(f) }

type mixed struct{}

func (mixed) Annotated(p []byte) { use(len(p)) }

func (mixed) Unannotated(p []byte) { global = p }
