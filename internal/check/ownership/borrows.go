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

package ownership

import (
	"cmp"
	"fmt"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"

	"github.com/tochemey/gomemsafe/internal/catalog"
	"github.com/tochemey/gomemsafe/internal/directive"
	"github.com/tochemey/gomemsafe/internal/escape"
	"github.com/tochemey/gomemsafe/internal/lazyssa"
	"github.com/tochemey/gomemsafe/internal/report"
)

// EscapeID is the check ID of a borrowed parameter escaping the call.
const EscapeID = catalog.EscapingBorrow

// Diagnostic texts of MS502. The value's source text is the first %s after the ID, then the
// function, then what it borrows: the value itself, or the parameter the value was reached
// from.
const (
	msgEscapes       = "%s: %s escapes %s, which borrows %s: %s"
	msgBorrowsHere   = "%s borrows %s here"
	helpEscape       = "copy what must outlive %s, or drop //memsafe:borrows %s"
	itself           = "it"
	literalCapturing = "a function literal capturing %s"

	howStoredField   = "stored into a field"
	howStoredElement = "stored into an element"
	howStoredMap     = "stored into a map"
	howStoredGlobal  = "stored into a global"
	howStoredPointer = "stored through a pointer"
	howAppended      = "appended to a slice"
	howSent          = "sent on a channel"
	howReturned      = "returned"
	howGoroutine     = "passed to a goroutine"
	howCaptured      = "captured by a goroutine"
	howKept          = "passed to %s, which keeps it"
	howConsumed      = "passed to the consuming parameter %s of %s"
	howUnknown       = "passed to %s, which may keep it"
	howPanicked      = "passed to panic"
	howWideLiteral   = "captured by a function literal with too many parameters and captures to analyse"
)

// hows words each kind of escape. The kinds that pass the value on take the callee's text,
// and a consuming parameter its name first.
var hows = map[escape.Kind]string{
	escape.StoredIntoField:       howStoredField,
	escape.StoredIntoElement:     howStoredElement,
	escape.StoredIntoMap:         howStoredMap,
	escape.StoredIntoGlobal:      howStoredGlobal,
	escape.StoredThroughPointer:  howStoredPointer,
	escape.Appended:              howAppended,
	escape.Sent:                  howSent,
	escape.Returned:              howReturned,
	escape.PassedToGoroutine:     howGoroutine,
	escape.CapturedByGoroutine:   howCaptured,
	escape.PassedToKeeper:        howKept,
	escape.PassedToConsumer:      howConsumed,
	escape.PassedToUnknown:       howUnknown,
	escape.PassedToPanic:         howPanicked,
	escape.CapturedByWideLiteral: howWideLiteral,
}

// borrowed returns the contracts of this package that borrow a parameter, in source order.
func borrowed(dir *directive.Result) []*directive.Contract {
	var out []*directive.Contract

	for _, c := range dir.Contracts {
		if c.Borrows != 0 {
			out = append(out, c)
		}
	}

	return out
}

// borrows reports every escape of the parameters fn's contract borrows, in fn and in the
// function literals that captured them.
func (c *checker) borrows(ssaRes *lazyssa.Result, contract *directive.Contract) {
	// The contract's function is declared in this package, so the program has it.
	fn := ssaRes.Func(contract.Func)
	a := escape.Analyze(fn, c.sum)

	for i, param := range fn.Params {
		if !contract.Borrows.Has(i) {
			continue
		}

		for _, e := range firstPerLine(c.pass.Fset, a.Escapes(i), param) {
			c.reportEscape(contract.Func, param, e)
		}
	}
}

// firstPerLine keeps one escape per line: the first, unless a later one on the line carries
// the parameter itself, which reads better than a value derived from it.
func firstPerLine(fset *token.FileSet, escapes []escape.Escape, param *ssa.Parameter) []escape.Escape {
	var (
		out  []escape.Escape
		at   = make(map[int]int)
		line int
	)

	for _, e := range escapes {
		line = fset.Position(e.At.Pos()).Line

		i, seen := at[line]
		if !seen {
			at[line] = len(out)
			out = append(out, e)

			continue
		}

		if e.Value == param && out[i].Value != param {
			out[i] = e
		}
	}

	return out
}

// escapePos returns where to report e: at the operand carrying the value, or, for a capture,
// at the go statement or the function literal, which go/ssa may give its closure no position
// for.
func (c *checker) escapePos(e escape.Escape) token.Pos {
	if captured(e) {
		return cmp.Or(e.Instr.Pos(), e.Value.(*ssa.MakeClosure).Fn.(*ssa.Function).Syntax().Pos())
	}

	return c.loc.UsePos(e.At, e.Value, e.At.Pos())
}

func (c *checker) reportEscape(fn *types.Func, param *ssa.Parameter, e escape.Escape) {
	name, borrows := c.escapeName(e, param)

	report.Report(c.pass, analysis.Diagnostic{
		Pos:            c.escapePos(e),
		Category:       EscapeID.String(),
		Message:        fmt.Sprintf(msgEscapes, EscapeID, name, fn.Name(), borrows, c.how(e)),
		Related:        []analysis.RelatedInformation{{Pos: fn.Pos(), Message: fmt.Sprintf(msgBorrowsHere, fn.Name(), param.Name())}},
		SuggestedFixes: []analysis.SuggestedFix{{Message: fmt.Sprintf(helpEscape, fn.Name(), param.Name())}},
	})
}

// escapeName names the escaping value, and the borrowed parameter it carries: "it" when the
// value is the parameter itself, or a capture of it.
func (c *checker) escapeName(e escape.Escape, param *ssa.Parameter) (name, borrows string) {
	if _, isClosure := e.Value.(*ssa.MakeClosure); isClosure {
		if captured(e) {
			return param.Name(), itself
		}

		return fmt.Sprintf(literalCapturing, param.Name()), param.Name()
	}

	name = valueName(c.loc.Operand(e.At, e.Value))
	if name == param.Name() {
		return name, itself
	}

	return name, param.Name()
}

// captured reports whether e is a capture by a function literal that is not analysed: one a
// goroutine starts, or one too wide.
func captured(e escape.Escape) bool {
	return e.Kind == escape.CapturedByGoroutine || e.Kind == escape.CapturedByWideLiteral
}

// how words the kind of escape, naming the callee for the kinds that pass the value on.
func (c *checker) how(e escape.Escape) string {
	text := hows[e.Kind]

	if e.Kind == escape.PassedToConsumer {
		return fmt.Sprintf(text, paramAt(e.Callee, e.Arg).Name(), c.calleeText(e.Instr.(ssa.CallInstruction)))
	}

	if e.Kind == escape.PassedToKeeper || e.Kind == escape.PassedToUnknown {
		return fmt.Sprintf(text, c.calleeText(e.Instr.(ssa.CallInstruction)))
	}

	return text
}
