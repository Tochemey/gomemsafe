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
	"testing"

	"github.com/tochemey/gomemsafe/internal/effect"
	"github.com/tochemey/gomemsafe/internal/summary"
)

func TestFactString(t *testing.T) {
	for _, tc := range []struct {
		fact summary.Fact
		want string
	}{
		{summary.Fact{Invalidates: effect.ArgSet(0b101)}, "invalidates(0x5)"},
		{summary.Fact{View: effect.View{Arg: 1, EndedBy: effect.Write}}, "viewof(1: 0x2)"},
		{summary.Fact{Events: []effect.Event{effect.Clear, 0, effect.All}}, "ends(0: 0x4, 2: 0xf)"},
		{summary.Fact{Consumes: 0b10}, "consumes(0x2)"},
		{summary.Fact{Syncs: true}, "syncs"},
		{summary.Fact{Consumes: 0b1, Syncs: true}, "consumes(0x1) syncs"},
		{summary.Fact{Retains: 0b1, Returns: 0b10}, "retains(0x1) returns(0x2)"},
		{
			summary.Fact{Invalidates: 1, View: effect.View{EndedBy: effect.Read}, Events: []effect.Event{effect.Read}},
			"invalidates(0x1) viewof(0: 0x1) ends(0: 0x1)",
		},
	} {
		if got := tc.fact.String(); got != tc.want {
			t.Errorf("String() = %q, want %q", got, tc.want)
		}
	}
}

func TestOwnedFactString(t *testing.T) {
	if got := (&summary.OwnedFact{Owned: true}).String(); got != "owned" {
		t.Fatalf("String() = %q, want owned", got)
	}
}
