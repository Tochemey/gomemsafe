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
	"go/types"
	"testing"
)

// TestKeptKnowsTheLibraryCalleesThatKeepNothing checks the table against the standard library:
// every entry names a function or an interface method that exists, with the retained and
// returned arguments the table gives it.
func TestKeptKnowsTheLibraryCalleesThatKeepNothing(t *testing.T) {
	for _, tc := range []struct {
		fn               *types.Func
		retains, returns ArgSet
	}{
		{interfaceMethod(t, "io", "Reader", "Read"), 0, 0},
		{interfaceMethod(t, "io", "Writer", "Write"), 0, 0},
		{interfaceMethod(t, "io", "ReaderAt", "ReadAt"), 0, 0},
		{interfaceMethod(t, "io", "WriterAt", "WriteAt"), 0, 0},
		{interfaceMethod(t, "io", "ByteReader", "ReadByte"), 0, 0},
		{interfaceMethod(t, "io", "ByteScanner", "UnreadByte"), 0, 0},
		{interfaceMethod(t, "io", "ByteWriter", "WriteByte"), 0, 0},
		{interfaceMethod(t, "io", "RuneReader", "ReadRune"), 0, 0},
		{interfaceMethod(t, "io", "RuneScanner", "UnreadRune"), 0, 0},
		{interfaceMethod(t, "io", "StringWriter", "WriteString"), 0, 0},
		{interfaceMethod(t, "io", "Closer", "Close"), 0, 0},
		{interfaceMethod(t, "io", "Seeker", "Seek"), 0, 0},
		{interfaceMethod(t, "io", "ReaderFrom", "ReadFrom"), 0, 0},
		{interfaceMethod(t, "io", "WriterTo", "WriteTo"), 0, 0},
		{function(t, "fmt", "Println"), 0, 0},
		{function(t, "fmt", "Fprintf"), 0, 0},
		{function(t, "fmt", "Sprint"), 0, 0},
		{function(t, "fmt", "Appendf"), 0, 1 << 0},
		{function(t, "fmt", "Errorf"), 0, 1 << 1},
		{function(t, "sort", "Slice"), 0, 0},
		{function(t, "encoding/json", "Marshal"), 0, 0},
		{function(t, "encoding/json", "Unmarshal"), 0, 0},
		{method(t, "encoding/json", "Encoder", "Encode"), 0, 0},
		{method(t, "encoding/json", "Decoder", "Decode"), 0, 0},
		{interfaceMethod(t, "context", "Context", "Deadline"), 0, 0},
		{interfaceMethod(t, "context", "Context", "Done"), 0, 0},
		{interfaceMethod(t, "context", "Context", "Err"), 0, 0},
		{interfaceMethod(t, "context", "Context", "Value"), 0, 0},
	} {
		retains, returns, known := Kept(tc.fn)
		if !known || retains != tc.retains || returns != tc.returns {
			t.Errorf("Kept(%s) = %#x, %#x, %v; want %#x, %#x, true", tc.fn.FullName(), uint64(retains), uint64(returns), known,
				uint64(tc.retains), uint64(tc.returns))
		}
	}

	for name := range kept {
		if !knownName(t, name) {
			t.Errorf("%s is in the table but not checked above", name)
		}
	}
}

func TestKeptIgnoresOtherFunctions(t *testing.T) {
	if _, _, known := Kept(method(t, "sync", "Pool", "Put")); known {
		t.Error("(*sync.Pool).Put is known to keep nothing")
	}

	if _, _, known := Kept(nil); known {
		t.Error("Kept(nil) knows something")
	}
}

// knownName reports whether name is one of the table's entries a test above resolves: the
// interface methods and functions by their full names.
func knownName(t *testing.T, name string) bool {
	t.Helper()

	for _, known := range []string{
		"(io.Reader).Read", "(io.Writer).Write", "(io.ReaderAt).ReadAt", "(io.WriterAt).WriteAt",
		"(io.ByteReader).ReadByte", "(io.ByteScanner).UnreadByte", "(io.ByteWriter).WriteByte", "(io.RuneReader).ReadRune",
		"(io.RuneScanner).UnreadRune", "(io.StringWriter).WriteString", "(io.Closer).Close", "(io.Seeker).Seek",
		"(io.ReaderFrom).ReadFrom", "(io.WriterTo).WriteTo",
		"fmt.Append", "fmt.Appendf", "fmt.Appendln", "fmt.Errorf", "fmt.Fprint", "fmt.Fprintf", "fmt.Fprintln",
		"fmt.Print", "fmt.Printf", "fmt.Println", "fmt.Sprint", "fmt.Sprintf", "fmt.Sprintln",
		"sort.Slice", "sort.SliceIsSorted", "sort.SliceStable",
		"encoding/json.Marshal", "encoding/json.MarshalIndent", "encoding/json.Unmarshal",
		"(*encoding/json.Encoder).Encode", "(*encoding/json.Decoder).Decode",
		"(context.Context).Deadline", "(context.Context).Done", "(context.Context).Err", "(context.Context).Value",
	} {
		if known == name {
			return true
		}
	}

	return false
}

// interfaceMethod returns the method name of the interface type typ of package path.
func interfaceMethod(t *testing.T, path, typ, name string) *types.Func {
	t.Helper()

	iface := importPackage(t, path).Scope().Lookup(typ).Type().Underlying().(*types.Interface)

	for i := range iface.NumMethods() {
		if m := iface.Method(i); m.Name() == name {
			return m
		}
	}

	t.Fatalf("%s.%s has no method %s", path, typ, name)

	return nil
}

// function returns the function name of package path.
func function(t *testing.T, path, name string) *types.Func {
	t.Helper()

	fn, ok := importPackage(t, path).Scope().Lookup(name).(*types.Func)
	if !ok {
		t.Fatalf("%s.%s is not a function", path, name)
	}

	return fn
}
