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

// TestReportsOverwrittenSlices checks every use of the appended fixture, and that each
// finding carries its ID, the append, and a help text naming the capped prefix.
func TestReportsOverwrittenSlices(t *testing.T) {
	results := analysistest.Run(t, analysistest.TestData(), Analyzer, "appended")
	for _, r := range results {
		for _, d := range r.Diagnostics {
			if d.Category != AppendID.String() || len(d.Related) != 1 || len(d.SuggestedFixes) != 1 ||
				!strings.Contains(d.Related[0].Message, "append on its prefix") || !strings.Contains(d.SuggestedFixes[0].Message, "capped prefix") {
				t.Errorf("%q: category %q, related %v, fixes %v; want %s, the append, and the help",
					d.Message, d.Category, d.Related, d.SuggestedFixes, AppendID)
			}
		}
	}
}

func TestStaysSilentOnSlicesStillIntact(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "appendedfalsepos")
}

// TestFollowsEverySliceWithTheFlag covers -ownership.appendalias on a package that annotates
// nothing.
func TestFollowsEverySliceWithTheFlag(t *testing.T) {
	enableAppendAlias(t)

	analysistest.Run(t, analysistest.TestData(), Analyzer, "appendalias")
}

// TestSuggestsTheCappedPrefix covers the help text, including when the prefix is not in the
// source.
func TestSuggestsTheCappedPrefix(t *testing.T) {
	if got := cappedText(nil); got != cappedPrefix {
		t.Errorf("cappedText(nil) = %q, want %q", got, cappedPrefix)
	}
}

// enableAppendAlias sets -ownership.appendalias for the rest of the test, then restores it.
func enableAppendAlias(t *testing.T) {
	t.Helper()

	previous := Analyzer.Flags.Lookup(AppendAliasFlag).Value.String()
	if err := Analyzer.Flags.Set(AppendAliasFlag, "true"); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := Analyzer.Flags.Set(AppendAliasFlag, previous); err != nil {
			t.Error(err)
		}
	})
}
