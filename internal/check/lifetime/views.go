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
	"fmt"

	"golang.org/x/tools/go/analysis"

	"github.com/tochemey/gomemsafe/internal/effect"
	"github.com/tochemey/gomemsafe/internal/invalidation"
	"github.com/tochemey/gomemsafe/internal/prefilter"
	"github.com/tochemey/gomemsafe/internal/report"
	"github.com/tochemey/gomemsafe/internal/summary"
)

// Diagnostic texts of MS302. The view's source text is the first %s after the ID, then the
// ending call, then the view's owner.
const (
	msgUseAfterViewEnded = "%s: %s used after %s ended the views into %s"
	msgViewFrom          = "%s is a view into %s from %s"
	msgViewEndedHere     = "%s ends the views into %s here"
	helpCopyView         = "copy %s with bytes.Clone before %s, or move %s after the last use of %s"
)

// checkedFuncs returns, in source order, the function declarations worth checking: those the
// summary analyzer marks as calling an invalidating function, and those that call both a view
// and a call that can end one. A function with a view but no such call has nothing to report,
// because views are followed within one function only.
func checkedFuncs(pre *prefilter.Result, sum *summary.Result) []*prefilter.Func {
	triggered := make(map[*prefilter.Func]bool, len(sum.Triggered))
	for _, f := range sum.Triggered {
		triggered[f] = true
	}

	var out []*prefilter.Func

	for _, f := range pre.Funcs {
		if triggered[f] || callsViewAndEvent(f) {
			out = append(out, f)
		}
	}

	return out
}

func callsViewAndEvent(f *prefilter.Func) bool {
	view, event := false, false

	for _, callee := range f.Callees {
		_, isView := effect.ViewOf(callee)
		view = view || isView
		event = event || effect.EventsOf(callee) != 0
	}

	return view && event
}

// reportView reports a view used after a call on its owner ended it, with the call that made
// the view and the call that ended it as related positions.
func (c *checker) reportView(u invalidation.Use) {
	viewSite := invalidation.Site{Call: u.View.Call, Callee: u.View.Callee}

	name := valueName(c.useOperand(u))
	owner := exprText(c.siteArg(viewSite, 0))
	made, ended := c.funText(viewSite), c.funText(u.Site)

	report.Report(c.pass, analysis.Diagnostic{
		Pos:      c.usePos(u),
		Category: ViewID.String(),
		Message:  fmt.Sprintf(msgUseAfterViewEnded, ViewID, name, ended, owner),
		Related: []analysis.RelatedInformation{
			{Pos: u.View.Call.Pos(), Message: fmt.Sprintf(msgViewFrom, name, owner, made)},
			{Pos: u.Site.Call.Pos(), Message: fmt.Sprintf(msgViewEndedHere, ended, owner)},
		},
		SuggestedFixes: []analysis.SuggestedFix{{Message: fmt.Sprintf(helpCopyView, name, ended, ended, name)}},
	})
}
