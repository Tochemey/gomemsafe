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

// Full names of the callees known to keep nothing of their arguments beyond the call, or to
// return memory of one, where their bodies cannot show it: the methods of the io interfaces,
// which transfer bytes within the call and whose documentation forbids retaining the buffer,
// and functions that hold their arguments in a pooled printer or in reflection for the length
// of the call only.
const (
	ioReaderRead              = "(io.Reader).Read"
	ioWriterWrite             = "(io.Writer).Write"
	ioReaderAtReadAt          = "(io.ReaderAt).ReadAt"
	ioWriterAtWriteAt         = "(io.WriterAt).WriteAt"
	ioByteReaderReadByte      = "(io.ByteReader).ReadByte"
	ioByteScannerUnreadByte   = "(io.ByteScanner).UnreadByte"
	ioByteWriterWriteByte     = "(io.ByteWriter).WriteByte"
	ioRuneReaderReadRune      = "(io.RuneReader).ReadRune"
	ioRuneScannerUnreadRune   = "(io.RuneScanner).UnreadRune"
	ioStringWriterWriteString = "(io.StringWriter).WriteString"
	ioCloserClose             = "(io.Closer).Close"
	ioSeekerSeek              = "(io.Seeker).Seek"
	ioReaderFromReadFrom      = "(io.ReaderFrom).ReadFrom"
	ioWriterToWriteTo         = "(io.WriterTo).WriteTo"

	fmtAppend   = "fmt.Append"
	fmtAppendf  = "fmt.Appendf"
	fmtAppendln = "fmt.Appendln"
	fmtErrorf   = "fmt.Errorf"
	fmtFprint   = "fmt.Fprint"
	fmtFprintf  = "fmt.Fprintf"
	fmtFprintln = "fmt.Fprintln"
	fmtPrint    = "fmt.Print"
	fmtPrintf   = "fmt.Printf"
	fmtPrintln  = "fmt.Println"
	fmtSprint   = "fmt.Sprint"
	fmtSprintf  = "fmt.Sprintf"
	fmtSprintln = "fmt.Sprintln"

	contextDeadline = "(context.Context).Deadline"
	contextDone     = "(context.Context).Done"
	contextErr      = "(context.Context).Err"
	contextValue    = "(context.Context).Value"

	sortSlice         = "sort.Slice"
	sortSliceIsSorted = "sort.SliceIsSorted"
	sortSliceStable   = "sort.SliceStable"

	jsonMarshal       = "encoding/json.Marshal"
	jsonMarshalIndent = "encoding/json.MarshalIndent"
	jsonUnmarshal     = "encoding/json.Unmarshal"
	jsonEncode        = "(*encoding/json.Encoder).Encode"
	jsonDecode        = "(*encoding/json.Decoder).Decode"
)

// kept maps a callee to what it keeps of its arguments. Keyed by types.Func.FullName. A
// callee listed with nothing keeps nothing: that is the point of listing it.
var kept = map[string]keep{
	// "Implementations must not retain p." (io.Reader, io.ReaderAt, io.Writer, io.WriterAt);
	// the other io methods transfer bytes within the call, with nothing to keep.
	ioReaderRead:              {},
	ioWriterWrite:             {},
	ioReaderAtReadAt:          {},
	ioWriterAtWriteAt:         {},
	ioByteReaderReadByte:      {},
	ioByteScannerUnreadByte:   {},
	ioByteWriterWriteByte:     {},
	ioRuneReaderReadRune:      {},
	ioRuneScannerUnreadRune:   {},
	ioStringWriterWriteString: {},
	ioCloserClose:             {},
	ioSeekerSeek:              {},
	ioReaderFromReadFrom:      {},
	ioWriterToWriteTo:         {},
	// The printers hold their arguments in a pooled state they clear before returning it.
	fmtFprint:   {},
	fmtFprintf:  {},
	fmtFprintln: {},
	fmtPrint:    {},
	fmtPrintf:   {},
	fmtPrintln:  {},
	fmtSprint:   {},
	fmtSprintf:  {},
	fmtSprintln: {},
	// The appenders return the slice they are given, grown; Errorf may wrap an argument.
	fmtAppend:   {returns: 1 << 0},
	fmtAppendf:  {returns: 1 << 0},
	fmtAppendln: {returns: 1 << 0},
	fmtErrorf:   {returns: 1 << 1},
	// A context reports on itself: its methods hand out its channel, deadline, error, and
	// values, and keep nothing of the caller's.
	contextDeadline: {},
	contextDone:     {},
	contextErr:      {},
	contextValue:    {},
	// Sorting holds the slice in reflection for the length of the call.
	sortSlice:         {},
	sortSliceIsSorted: {},
	sortSliceStable:   {},
	// Encoding walks the value in reflection, with a pooled state cleared before it returns.
	jsonMarshal:       {},
	jsonMarshalIndent: {},
	jsonUnmarshal:     {},
	jsonEncode:        {},
	jsonDecode:        {},
}

// keep is what a callee keeps of its arguments.
type keep struct {
	retains, returns ArgSet
}

// Kept returns what fn is known to keep of its arguments, the receiver first: the arguments
// kept beyond the call, outside its results, and those whose memory a result may share. It
// reports whether fn is known at all.
func Kept(fn *types.Func) (retains, returns ArgSet, known bool) {
	if fn == nil {
		return 0, 0, false
	}

	k, ok := kept[fn.Origin().FullName()]

	return k.retains, k.returns, ok
}
