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

package lifetime

import (
	"go/token"
	"os"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/tochemey/gomemsafe/internal/lazyssa"
)

func TestReportsViewsUsedAfterTheyEnded(t *testing.T) {
	results := analysistest.Run(t, analysistest.TestData(), Analyzer, "views")
	for _, r := range results {
		for _, d := range r.Diagnostics {
			if d.Category != ViewID.String() {
				t.Errorf("%q: category %q, want %s", d.Message, d.Category, ViewID)
			}

			if len(d.Related) != 2 || len(d.SuggestedFixes) != 1 || d.SuggestedFixes[0].Message == "" {
				t.Errorf("%q: %d related positions and %d fixes, want 2, and 1 with help",
					d.Message, len(d.Related), len(d.SuggestedFixes))
			}
		}
	}
}

// TestReportsAViewTakenBackAsAnInvalidation covers a view returned to a sync.Pool: MS301, not
// MS302.
func TestReportsAViewTakenBackAsAnInvalidation(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "viewsput")
}

// TestBuildsNoSSAForViewsNothingEnds covers a package that makes views but never calls anything
// that ends one: views are followed within one function, so there is nothing to check. Its
// dependencies may need SSA of their own (io returns buffers to a sync.Pool), so the count is
// compared with a package that imports the same and makes no view.
func TestBuildsNoSSAForViewsNothingEnds(t *testing.T) {
	baseline := ssaBuilds(t, "viewsnone")

	if got := ssaBuilds(t, "viewsonly"); got != baseline {
		t.Fatalf("built SSA for %d packages, want %d as without views", got, baseline)
	}
}

// TestReportsViewsOfDirectivesAndSummaries covers views that //memsafe:viewof declares or a
// helper in another package returns, and the calls that //memsafe:invalidates declares or a
// helper makes. The help text suggests bytes.Clone only for a []byte view.
func TestReportsViewsOfDirectivesAndSummaries(t *testing.T) {
	results := analysistest.Run(t, analysistest.TestData(), Analyzer, "viewuser")
	for _, r := range results {
		for _, d := range r.Diagnostics {
			clone := strings.Contains(d.SuggestedFixes[0].Message, "bytes.Clone")
			if rows := strings.HasPrefix(d.Message, "MS302: rows "); clone == rows {
				t.Errorf("%q: help %q", d.Message, d.SuggestedFixes[0].Message)
			}
		}
	}
}

func TestStaysSilentOnCorrectViewUse(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "viewsfalsepos")
}

// TestPointsAtTheView checks that each finding's column is where the view it names is written,
// and that the related positions are the call that made the view and the call that ended it,
// at their opening parentheses, where SSA places calls.
func TestPointsAtTheView(t *testing.T) {
	message := regexp.MustCompile(`^MS302: (\w+) used after (\S+) ended the views into ([\w.]+)$`)
	related := regexp.MustCompile(`^\w+ is a view into [\w.]+ from (\S+)$`)

	results := analysistest.Run(t, analysistest.TestData(), Analyzer, "views", "viewuser")
	for _, r := range results {
		for _, d := range r.Diagnostics {
			m := message.FindStringSubmatch(d.Message)
			if m == nil {
				t.Errorf("unexpected message %q", d.Message)
				continue
			}

			if got := sourceAt(t, r.Pass.Fset.Position(d.Pos)); !strings.HasPrefix(got, m[1]) {
				t.Errorf("%q: points at %q, want the view %s", d.Message, got, m[1])
			}

			made := related.FindStringSubmatch(d.Related[0].Message)
			if made == nil {
				t.Errorf("%q: unexpected related message %q", d.Message, d.Related[0].Message)
				continue
			}

			checkCallAt(t, d.Message, r.Pass.Fset.Position(d.Related[0].Pos), made[1])
			checkCallAt(t, d.Message, r.Pass.Fset.Position(d.Related[1].Pos), m[2])
		}
	}
}

// checkCallAt checks that posn is the opening parenthesis of a call of fun.
func checkCallAt(t *testing.T, message string, posn token.Position, fun string) {
	t.Helper()

	src, err := os.ReadFile(posn.Filename)
	if err != nil {
		t.Fatal(err)
	}

	line := strings.Split(string(src), "\n")[posn.Line-1]
	if before := line[:posn.Column-1]; !strings.HasSuffix(before, fun) || line[posn.Column-1] != '(' {
		t.Errorf("%q: related position at %q, want the call of %s", message, line[posn.Column-1:], fun)
	}
}

// ssaBuilds returns how many packages the check lowered to SSA while analysing pkg and its
// dependencies.
func ssaBuilds(t *testing.T, pkg string) int64 {
	t.Helper()

	before := lazyssa.Builds()

	analysistest.Run(t, analysistest.TestData(), Analyzer, pkg)

	return lazyssa.Builds() - before
}
