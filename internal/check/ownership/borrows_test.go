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
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestReportsEscapingBorrows checks every escape of the borrowed fixture, and that each
// finding carries its ID, the declaration that borrows, and a help text.
func TestReportsEscapingBorrows(t *testing.T) {
	results := analysistest.Run(t, analysistest.TestData(), Analyzer, "borrowed")
	for _, r := range results {
		for _, d := range r.Diagnostics {
			if d.Category != EscapeID.String() || len(d.Related) != 1 || len(d.SuggestedFixes) != 1 ||
				!strings.Contains(d.Related[0].Message, "borrows") || !strings.Contains(d.SuggestedFixes[0].Message, "//memsafe:borrows") {
				t.Errorf("%q: category %q, related %v, fixes %v; want %s, the borrowing declaration, and the help",
					d.Message, d.Category, d.Related, d.SuggestedFixes, EscapeID)
			}
		}
	}
}

func TestStaysSilentOnBorrowsKeptWithinTheCall(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "borrowedfalsepos")
}

// TestChecksBorrowsWithoutOwnership covers a package that declares a borrow and nothing else:
// no owned type and no consuming parameter anywhere, so MS501 has nothing to do and MS502
// still runs.
func TestChecksBorrowsWithoutOwnership(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "borrowonly")
}
