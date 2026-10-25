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

package unsafeflow_test

import (
	"go/token"
	"os"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/tochemey/gomemsafe/internal/check/unsafeflow"
	"github.com/tochemey/gomemsafe/internal/lazyssa"
)

func TestReportsStringsUsedAfterAWriteIntoTheirBytes(t *testing.T) {
	results := analysistest.Run(t, analysistest.TestData(), unsafeflow.Analyzer, "mutation")
	checkCategoryAndHelp(t, results, unsafeflow.MutationID)

	for _, r := range results {
		for _, d := range r.Diagnostics {
			if len(d.Related) != 2 || d.Related[0].Message == "" || d.Related[1].Message == "" {
				t.Errorf("%q: related %v, want the view and the write", d.Message, d.Related)
			}
		}
	}
}

func TestStaysSilentOnStringsWhoseBytesNoWriteChanges(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), unsafeflow.Analyzer, "mutationfalsepos")
}

// TestPointsAtTheStringTheViewAndTheWrite checks that each finding is where the string it names
// is written, that the first related position is the unsafe.String or unsafe.Slice call, and
// that the second is on the line of the write.
func TestPointsAtTheStringTheViewAndTheWrite(t *testing.T) {
	name := regexp.MustCompile(`^MS102: (\S+) used after`)
	write := regexp.MustCompile(`\[|copy\(|clear\(|^\s*a = `)

	results := analysistest.Run(t, analysistest.TestData(), unsafeflow.Analyzer, "mutation")
	for _, r := range results {
		for _, d := range r.Diagnostics {
			m := name.FindStringSubmatch(d.Message)
			if m == nil {
				t.Fatalf("unexpected message %q", d.Message)
			}

			if got := lineFrom(t, r.Pass.Fset.Position(d.Pos)); !strings.HasPrefix(got, m[1]) {
				t.Errorf("%q: points at %q, want %s", d.Message, got, m[1])
			}

			view := r.Pass.Fset.Position(d.Related[0].Pos)
			if got := lineFrom(t, view); !strings.HasPrefix(got, "(") ||
				!regexp.MustCompile(`unsafe\.(String|Slice)$`).MatchString(lineUpTo(t, view)) {
				t.Errorf("%q: view at %q, want the unsafe.String or unsafe.Slice call", d.Message, got)
			}

			written := r.Pass.Fset.Position(d.Related[1].Pos)
			if line := lineUpTo(t, written) + lineFrom(t, written); !write.MatchString(line) {
				t.Errorf("%q: write at %q, want a line that writes into the bytes", d.Message, line)
			}
		}
	}
}

// TestBuildsNoSSAWithoutStringViews covers packages that use package unsafe but make no string
// view MS102 can follow, such as a []byte of a constant string: nothing is lowered to SSA.
func TestBuildsNoSSAWithoutStringViews(t *testing.T) {
	before := lazyssa.Builds()

	analysistest.Run(t, analysistest.TestData(), unsafeflow.Analyzer, "roundtrip", "header", "constview")

	if got := lazyssa.Builds() - before; got != 0 {
		t.Fatalf("built SSA for %d packages, want 0", got)
	}
}

// lineFrom returns the source text from posn to the end of its line.
func lineFrom(t *testing.T, posn token.Position) string {
	t.Helper()

	return sourceLine(t, posn)[posn.Column-1:]
}

// lineUpTo returns the source text of posn's line before posn.
func lineUpTo(t *testing.T, posn token.Position) string {
	t.Helper()

	return sourceLine(t, posn)[:posn.Column-1]
}

func sourceLine(t *testing.T, posn token.Position) string {
	t.Helper()

	src, err := os.ReadFile(posn.Filename)
	if err != nil {
		t.Fatal(err)
	}

	return strings.Split(string(src), "\n")[posn.Line-1]
}
