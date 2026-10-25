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
// that cannot be derived from the callee's code. There are two kinds.
//
// An invalidation takes an argument back, such as sync.Pool.Put handing its argument back to
// the pool, so the caller must not use it afterwards (MS301). Known invalidations take
// precedence over computed summaries (package summary), which derive the invalidations of every
// function that calls one of these.
//
// A view is a result that points into the receiver's buffer, such as bytes.Buffer.Bytes. It
// stays valid only until the receiver's next call of certain kinds, its events: a read, a
// write, a clear, or a ReadLine (MS302). Every view and every event cites the documentation it comes
// from. Where the documentation does not say that a call ends a view, the call has no event,
// even if the implementation overwrites the buffer: a missed bug is better than a false one.
// Known views and events take precedence over computed summaries too, which derive the views
// and events of code that calls these.
//
// To teach gomemsafe another effect, add the callee's full name (as types.Func.FullName prints
// it) to known, views, or events. Argument indexes count the receiver as 0, matching the
// argument order of an SSA call. Each new entry needs a test in effect_test.go and a fixture
// under internal/check/lifetime/testdata.
package effect

import "go/types"

// Full names of the callees with a known effect, as types.Func.FullName prints them.
const (
	syncPoolPut = "(*sync.Pool).Put"

	bufferAvailableBuffer = "(*bytes.Buffer).AvailableBuffer"
	bufferBytes           = "(*bytes.Buffer).Bytes"
	bufferNext            = "(*bytes.Buffer).Next"
	bufferPeek            = "(*bytes.Buffer).Peek"
	bufferRead            = "(*bytes.Buffer).Read"
	bufferReadByte        = "(*bytes.Buffer).ReadByte"
	bufferReadBytes       = "(*bytes.Buffer).ReadBytes"
	bufferReadFrom        = "(*bytes.Buffer).ReadFrom"
	bufferReadRune        = "(*bytes.Buffer).ReadRune"
	bufferReadString      = "(*bytes.Buffer).ReadString"
	bufferReset           = "(*bytes.Buffer).Reset"
	bufferTruncate        = "(*bytes.Buffer).Truncate"
	bufferWrite           = "(*bytes.Buffer).Write"
	bufferWriteByte       = "(*bytes.Buffer).WriteByte"
	bufferWriteRune       = "(*bytes.Buffer).WriteRune"
	bufferWriteString     = "(*bytes.Buffer).WriteString"
	bufferWriteTo         = "(*bytes.Buffer).WriteTo"

	scannerBytes = "(*bufio.Scanner).Bytes"
	scannerScan  = "(*bufio.Scanner).Scan"

	readerPeek       = "(*bufio.Reader).Peek"
	readerRead       = "(*bufio.Reader).Read"
	readerReadByte   = "(*bufio.Reader).ReadByte"
	readerReadBytes  = "(*bufio.Reader).ReadBytes"
	readerReadLine   = "(*bufio.Reader).ReadLine"
	readerReadRune   = "(*bufio.Reader).ReadRune"
	readerReadSlice  = "(*bufio.Reader).ReadSlice"
	readerReadString = "(*bufio.Reader).ReadString"

	writerAvailableBuffer = "(*bufio.Writer).AvailableBuffer"
	writerReadFrom        = "(*bufio.Writer).ReadFrom"
	writerWrite           = "(*bufio.Writer).Write"
	writerWriteByte       = "(*bufio.Writer).WriteByte"
	writerWriteRune       = "(*bufio.Writer).WriteRune"
	writerWriteString     = "(*bufio.Writer).WriteString"
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

// views maps a callee whose first result is a view into its receiver to the events that end
// the view. Keyed by types.Func.FullName.
var views = map[string]Event{
	// "The slice is valid for use only until the next buffer modification (that is, only until
	// the next call to a method like Buffer.Read, Buffer.Write, Buffer.Reset, or
	// Buffer.Truncate)."
	bufferBytes: Read | Write | Clear,
	// "The slice is only valid until the next call to a read or write method."
	bufferNext: Read | Write,
	bufferPeek: Read | Write,
	// "The buffer is only valid until the next write operation on b."
	bufferAvailableBuffer: Write,
	writerAvailableBuffer: Write,
	// "The underlying array may point to data that will be overwritten by a subsequent call to
	// Scan."
	scannerBytes: Read,
	// "The bytes stop being valid at the next read call." (Peek), "The bytes stop being valid at
	// the next read." (ReadSlice)
	readerPeek:      Read,
	readerReadSlice: Read,
	// "The returned buffer is only valid until the next call to ReadLine."
	readerReadLine: LineRead,
}

// events maps a callee to the events a call of it is on its receiver. Keyed by
// types.Func.FullName.
var events = map[string]Event{
	// bytes.Buffer: its read and write methods, and Reset and Truncate, which only the Bytes
	// documentation names. Grow is none of these: no view's documentation mentions it. Peek,
	// UnreadByte, and UnreadRune do not modify the buffer.
	bufferRead:        Read,
	bufferReadByte:    Read,
	bufferReadBytes:   Read,
	bufferReadRune:    Read,
	bufferReadString:  Read,
	bufferNext:        Read,
	bufferWriteTo:     Read,
	bufferWrite:       Write,
	bufferWriteByte:   Write,
	bufferWriteRune:   Write,
	bufferWriteString: Write,
	bufferReadFrom:    Write,
	bufferReset:       Clear,
	bufferTruncate:    Clear,

	// bufio.Scanner: "data that will be overwritten by a subsequent call to Scan."
	scannerScan: Read,

	// bufio.Reader: its read operations. The UnreadByte documentation states that "Reader.Peek,
	// Reader.Discard, and Reader.WriteTo are not considered read operations."
	readerRead:       Read,
	readerReadByte:   Read,
	readerReadBytes:  Read,
	readerReadLine:   Read | LineRead,
	readerReadRune:   Read,
	readerReadSlice:  Read,
	readerReadString: Read,

	// bufio.Writer: its write operations. Flush sends the buffered data on but writes nothing
	// into the buffer.
	writerWrite:       Write,
	writerWriteByte:   Write,
	writerWriteRune:   Write,
	writerWriteString: Write,
	writerReadFrom:    Write,
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

// Event is a set of kinds of call on a buffer. A view is ended by the events listed for it.
type Event uint8

const (
	// Read is a read from the buffer, such as bytes.Buffer.Read or bufio.Scanner.Scan.
	Read Event = 1 << iota
	// Write is a write into the buffer, such as bytes.Buffer.Write.
	Write
	// Clear is bytes.Buffer.Reset or Truncate, which the Bytes documentation names but no other
	// view's does.
	Clear
	// LineRead is a call of bufio.Reader.ReadLine, the only event that ends its result.
	LineRead

	// All is every event. A call that a //memsafe:invalidates directive declares is all of them
	// on its argument, so it ends every view of it; a view that a //memsafe:viewof directive
	// declares is ended by any of them.
	All = Read | Write | Clear | LineRead
)

// View is what a call's first result points into: the buffer of call argument Arg. It stays
// valid until a call that is one of the events EndedBy on that argument. Every view is ended by
// some event, so the zero View is no view.
type View struct {
	Arg     int
	EndedBy Event
}

// Of returns the known invalidation of fn, if it has one.
func Of(fn *types.Func) (Invalidation, bool) {
	if fn == nil {
		return Invalidation{}, false
	}

	inv, ok := known[fn.Origin().FullName()]

	return inv, ok
}

// ViewOf returns the view fn returns, if fn's first result is a view into its receiver's
// buffer.
func ViewOf(fn *types.Func) (View, bool) {
	if fn == nil {
		return View{}, false
	}

	ended, ok := views[fn.Origin().FullName()]

	return View{Arg: 0, EndedBy: ended}, ok
}

// EventsOf returns the events that a call of fn is on its receiver: zero for most functions.
func EventsOf(fn *types.Func) Event {
	if fn == nil {
		return 0
	}

	return events[fn.Origin().FullName()]
}

// Has reports whether index i is in the set.
func (s ArgSet) Has(i int) bool { return i >= 0 && i < 64 && s&(1<<uint(i)) != 0 }
