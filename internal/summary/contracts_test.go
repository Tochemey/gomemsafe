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

package summary_test

import (
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/tochemey/gomemsafe/internal/summary"
)

// TestExportsInterfaceContractFacts checks that an interface method's contract is its fact, with
// what it leaves out kept by default, and that its calls read it, in the package and elsewhere.
func TestExportsInterfaceContractFacts(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), summary.Analyzer, "contractlib", "contractclient")
}

// TestInterfaceContractsListsThosePackageSees checks the interface methods with a contract: of
// the package and of what it imports, sorted, and not the methods with none.
func TestInterfaceContractsListsThosePackageSees(t *testing.T) {
	for _, r := range analysistest.Run(t, analysistest.TestData(), summary.Analyzer, "contractclient") {
		var names []string

		for _, fn := range r.Result.(*summary.Result).InterfaceContracts() {
			names = append(names, fn.FullName())
		}

		want := []string{
			"(contractclient.Local).Peek", "(contractlib.Store).Peek", "(contractlib.Store).Reset",
			"(contractlib.Store).View", "(contractlib.Store).Write",
		}

		if !slices.Equal(names, want) {
			t.Errorf("InterfaceContracts = %v, want %v", names, want)
		}
	}
}
