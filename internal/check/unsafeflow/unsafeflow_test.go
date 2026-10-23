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
	"os"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/tochemey/gomemsafe/internal/check/unsafeflow"
)

func TestReportsRoundTripsVetCannotSee(t *testing.T) {
	results := analysistest.Run(t, analysistest.TestData(), unsafeflow.Analyzer, "roundtrip")
	for _, r := range results {
		for _, d := range r.Diagnostics {
			if d.Category != string(unsafeflow.ID) {
				t.Errorf("%q: category %q, want %s", d.Message, d.Category, unsafeflow.ID)
			}

			if len(d.SuggestedFixes) != 1 || d.SuggestedFixes[0].Message == "" {
				t.Errorf("%q: no help text", d.Message)
			}
		}
	}
}

// TestPointsAtTheUintptr checks that each reinterpretation finding's column is where the
// uintptr it names is written: u in &u, not the &.
func TestPointsAtTheUintptr(t *testing.T) {
	name := regexp.MustCompile(`^MS101: \*?([\w.]+) is a uintptr`)

	results := analysistest.Run(t, analysistest.TestData(), unsafeflow.Analyzer, "roundtrip")
	for _, r := range results {
		for _, d := range r.Diagnostics {
			m := name.FindStringSubmatch(d.Message)
			if m == nil {
				continue
			}

			posn := r.Pass.Fset.Position(d.Pos)

			src, err := os.ReadFile(posn.Filename)
			if err != nil {
				t.Fatal(err)
			}

			line := strings.Split(string(src), "\n")[posn.Line-1]
			if !strings.HasPrefix(line[posn.Column-1:], m[1]) {
				t.Errorf("%s: points at %q, want %s", posn, line[posn.Column-1:], m[1])
			}
		}
	}
}

func TestStaysSilentOnCorrectCodeAndVetFindings(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), unsafeflow.Analyzer, "falsepos")
}

func TestHonoursSuppressions(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), unsafeflow.Analyzer, "suppress")
}
