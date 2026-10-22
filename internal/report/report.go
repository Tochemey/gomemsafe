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

// Package report is the one way checks emit diagnostics. It applies //memsafe:allow
// suppressions (package directive) by check ID, so every check honours them the same way.
// Checks must call Report instead of pass.Report.
package report

import (
	"golang.org/x/tools/go/analysis"

	"github.com/tochemey/gomemsafe/internal/directive"
)

// Report emits d unless a //memsafe:allow directive suppresses its Category at d.Pos.
func Report(pass *analysis.Pass, d analysis.Diagnostic) {
	dirs := pass.ResultOf[directive.Analyzer].(*directive.Result)
	if dirs.Suppressed(pass.Fset, d.Pos, d.Category) {
		return
	}

	pass.Report(d)
}
