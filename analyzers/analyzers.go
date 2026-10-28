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

// Package analyzers is the public API of gomemsafe: the checks, as standard go/analysis
// analyzers that any driver can run (go vet through cmd/gomemsafe, multichecker,
// golangci-lint, gopls).
//
// All returns every check; the analyzers each check depends on (directive parsing, the
// prefilter, lazy SSA, summaries) are pulled in through their Requires fields and need not be
// listed. To add a check, implement it under internal/check, give it an ID in
// internal/catalog, and append its analyzer to All.
package analyzers

import (
	"golang.org/x/tools/go/analysis"

	"github.com/tochemey/gomemsafe/internal/check/lifetime"
	"github.com/tochemey/gomemsafe/internal/check/ownership"
	"github.com/tochemey/gomemsafe/internal/check/sharing"
	"github.com/tochemey/gomemsafe/internal/check/unsafeflow"
	"github.com/tochemey/gomemsafe/internal/directive"
)

// All returns every gomemsafe check. Analyzers they depend on, such as the summary
// analyzer that exports facts, run automatically through Requires.
func All() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		directive.Analyzer, unsafeflow.Analyzer, lifetime.Analyzer, sharing.Analyzer, ownership.Analyzer,
	}
}

// Default returns the checks that run when no analyzer is selected. That is every check: an
// opt-in check, such as MS104, is an analyzer option that is off unless its flag is set
// (-unsafeflow.safety).
func Default() []*analysis.Analyzer {
	return All()
}
