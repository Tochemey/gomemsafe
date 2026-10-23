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
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/tochemey/gomemsafe/internal/check/unsafeflow"
)

func TestReportsUnsafeWithoutSafetyComment(t *testing.T) {
	enableSafety(t)

	results := analysistest.Run(t, analysistest.TestData(), unsafeflow.Analyzer, "safety")
	checkCategoryAndHelp(t, results, unsafeflow.SafetyID)
}

// TestSafetyIsOffByDefault checks the flag's declared default, which does not depend on what
// other tests set, and that nothing is reported while the flag is off.
func TestSafetyIsOffByDefault(t *testing.T) {
	if got := unsafeflow.Analyzer.Flags.Lookup(unsafeflow.SafetyFlag).DefValue; got != "false" {
		t.Fatalf("-%s defaults to %s, want false", unsafeflow.SafetyFlag, got)
	}

	analysistest.Run(t, analysistest.TestData(), unsafeflow.Analyzer, "safetydefault")
}

// enableSafety sets -unsafeflow.safety for the rest of the test, then restores its value.
func enableSafety(t *testing.T) {
	t.Helper()

	previous := unsafeflow.Analyzer.Flags.Lookup(unsafeflow.SafetyFlag).Value.String()
	if err := unsafeflow.Analyzer.Flags.Set(unsafeflow.SafetyFlag, "true"); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := unsafeflow.Analyzer.Flags.Set(unsafeflow.SafetyFlag, previous); err != nil {
			t.Error(err)
		}
	})
}
