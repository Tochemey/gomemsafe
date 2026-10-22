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

package frontend

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePosn(t *testing.T) {
	p, ok := parsePosn("C:/x/a.go:12:5")
	if !ok || p.file != "C:/x/a.go" || p.line != 12 || p.col != 5 {
		t.Fatalf("parsePosn = %+v, %v", p, ok)
	}

	for _, bad := range []string{"a.go", "a.go:3", "a.go:x:1", "a.go:1:y"} {
		if _, ok := parsePosn(bad); ok {
			t.Errorf("parsePosn(%q) succeeded", bad)
		}
	}
}

func TestLessPosnOrdersByFileLineColumn(t *testing.T) {
	ordered := []string{"a.go:1:9", "a.go:2:1", "a.go:2:3", "b.go:1:1"}
	for i := 1; i < len(ordered); i++ {
		if !lessPosn(ordered[i-1], ordered[i]) || lessPosn(ordered[i], ordered[i-1]) {
			t.Errorf("%s and %s are out of order", ordered[i-1], ordered[i])
		}
	}

	if !lessPosn("garbage", "zzz") {
		t.Error("unparsable positions do not fall back to string order")
	}
}

func TestDisplayPosnIsRelativeInsideWorkingDirectory(t *testing.T) {
	wd := t.TempDir()

	tests := []struct{ name, posn, want string }{
		{"inside", filepath.Join(wd, "a.go") + ":3:4", "a.go:3:4"},
		{"name starting with dots", filepath.Join(wd, "..a.go") + ":3:4", "..a.go:3:4"},
		{"outside", filepath.Join(filepath.Dir(wd), "other.go") + ":1:1", filepath.Join(filepath.Dir(wd), "other.go") + ":1:1"},
		{"unparsable", "junk", "junk"},
	}

	for _, tt := range tests {
		if got := displayPosn(tt.posn, wd); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}

	posn := filepath.Join(wd, "a.go") + ":3:4"
	if got := displayPosn(posn, ""); got != posn {
		t.Errorf("unknown working directory: %q, want the position unchanged", got)
	}
}

func TestRenderAlignsCaretUnderTabIndentedIdentifier(t *testing.T) {
	file := filepath.Join(t.TempDir(), "x.go")
	if err := os.WriteFile(file, []byte("package x\n\n\tbuf.WriteString(p)\n\tpool.Put(buf)\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	d := Diagnostic{Posn: file + ":3:2", Message: "MS301: buf used"}
	d.Related = append(d.Related, struct {
		Posn    string `json:"posn"`
		Message string `json:"message"`
	}{Posn: file + ":4:10", Message: "buf returned here"})
	d.SuggestedFixes = append(d.SuggestedFixes, struct {
		Message string `json:"message"`
	}{Message: "move it"})

	var out bytes.Buffer

	render(&printer{w: &out}, []Diagnostic{d, {Posn: file + ":99:1", Message: "past the end"}})

	want := file + ":3:2: MS301: buf used\n" +
		"    buf.WriteString(p)\n" +
		"    ^^^\n" +
		"  " + file + ":4:10: buf returned here\n" +
		"    pool.Put(buf)\n" +
		"  help: move it\n" +
		"\n" +
		file + ":99:1: past the end\n"
	if out.String() != want {
		t.Fatalf("render:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestSnippetSkipsUnreadableFiles(t *testing.T) {
	var out bytes.Buffer

	src := sourceCache{}
	src.snippet(&printer{w: &out}, filepath.Join(t.TempDir(), "missing.go")+":1:1", true)
	src.snippet(&printer{w: &out}, "junk", true)

	if out.Len() != 0 {
		t.Fatalf("printed %q for an unreadable file", out.String())
	}
}

func TestIdentWidthAndExpandTabs(t *testing.T) {
	if identWidth("buf.Write") != 3 || identWidth("(x") != 1 || identWidth("") != 1 {
		t.Fatal("identWidth miscounts")
	}

	if got := expandTabs("\ta\tb"); got != strings.Repeat(" ", tabWidth)+"a"+strings.Repeat(" ", tabWidth)+"b" {
		t.Fatalf("expandTabs = %q", got)
	}
}
