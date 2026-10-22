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
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// tabWidth is how many spaces a tab takes in rendered source lines.
const tabWidth = 4

// position is a parsed "file:line:column" string.
type position struct {
	file      string
	line, col int
}

// sourceCache reads each source file at most once.
type sourceCache map[string][]string

// render prints diagnostics in the Rust-like format of docs/design.md §6:
//
//	file.go:42:3: MS301: buf used after it was returned to sync.Pool
//	    buf.WriteString(p)
//	    ^^^
//	  file.go:40:2: buf returned to sync.Pool here
//	    pool.Put(buf)
//	  help: move pool.Put(buf) after the last use, or use `defer pool.Put(buf)`
func render(p *printer, diags []Diagnostic) {
	src := sourceCache{}

	// An unknown working directory only leaves positions absolute.
	wd, _ := os.Getwd()

	for i, d := range diags {
		if i > 0 {
			p.printf("\n")
		}

		p.printf("%s: %s\n", displayPosn(d.Posn, wd), d.Message)
		src.snippet(p, d.Posn, true)

		for _, r := range d.Related {
			p.printf("  %s: %s\n", displayPosn(r.Posn, wd), r.Message)
			src.snippet(p, r.Posn, false)
		}

		for _, f := range d.SuggestedFixes {
			p.printf("  help: %s\n", f.Message)
		}
	}
}

func parsePosn(s string) (position, bool) {
	// The file name itself may contain colons, so split from the right.
	i := strings.LastIndexByte(s, ':')
	if i < 0 {
		return position{}, false
	}

	j := strings.LastIndexByte(s[:i], ':')
	if j < 0 {
		return position{}, false
	}

	line, lineErr := strconv.Atoi(s[j+1 : i])
	col, colErr := strconv.Atoi(s[i+1:])

	if lineErr != nil || colErr != nil {
		return position{}, false
	}

	return position{file: s[:j], line: line, col: col}, true
}

func lessPosn(a, b string) bool {
	pa, okA := parsePosn(a)
	pb, okB := parsePosn(b)

	switch {
	case !okA || !okB:
		return a < b
	case pa.file != pb.file:
		return pa.file < pb.file
	case pa.line != pb.line:
		return pa.line < pb.line
	}

	return pa.col < pb.col
}

// displayPosn shortens an absolute position to one relative to wd, the working directory,
// when the file is inside it. An empty wd leaves the position as it is.
func displayPosn(posn, wd string) string {
	p, ok := parsePosn(posn)
	if !ok || wd == "" {
		return posn
	}

	rel, err := filepath.Rel(wd, p.file)
	if err != nil || !filepath.IsLocal(rel) {
		return posn
	}

	return fmt.Sprintf("%s:%d:%d", rel, p.line, p.col)
}

// expandTabs widens tabs so that a caret line drawn under the result lines up.
func expandTabs(s string) string {
	return strings.ReplaceAll(s, "\t", strings.Repeat(" ", tabWidth))
}

// identWidth returns the length of the identifier at the start of s, or 1.
func identWidth(s string) int {
	n := 0

	for _, r := range s {
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}

		n++
	}

	return max(n, 1)
}

// lines returns the lines of file, or nil when it cannot be read: a missing snippet must not
// hide the diagnostic itself.
func (c sourceCache) lines(file string) []string {
	if ls, ok := c[file]; ok {
		return ls
	}

	var ls []string

	if f, err := os.Open(filepath.Clean(file)); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			ls = append(ls, sc.Text())
		}

		if sc.Err() != nil {
			ls = nil
		}

		_ = f.Close()
	}

	c[file] = ls

	return ls
}

// snippet prints the source line at posn without its indentation and, when caret is set, a
// line of carets under the identifier the position points at.
func (c sourceCache) snippet(p *printer, posn string, caret bool) {
	const indent = "    "

	pos, ok := parsePosn(posn)
	if !ok {
		return
	}

	ls := c.lines(pos.file)
	if pos.line < 1 || pos.line > len(ls) {
		return
	}

	raw := ls[pos.line-1]
	shown := expandTabs(raw)
	trimmed := strings.TrimLeftFunc(shown, unicode.IsSpace)
	p.printf("%s%s\n", indent, trimmed)

	// Columns count bytes of the raw line.
	col := pos.col - 1
	if !caret || col < 0 || col > len(raw) {
		return
	}

	pad := max(len(expandTabs(raw[:col]))-(len(shown)-len(trimmed)), 0)
	p.printf("%s%s%s\n", indent, strings.Repeat(" ", pad), strings.Repeat("^", identWidth(raw[col:])))
}
