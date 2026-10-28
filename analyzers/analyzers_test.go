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

package analyzers_test

import (
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/tochemey/gomemsafe/analyzers"
)

func TestAllIsAValidAnalyzerSet(t *testing.T) {
	all := analyzers.All()
	if err := analysis.Validate(all); err != nil {
		t.Fatal(err)
	}

	names := make(map[string]bool)
	for _, a := range all {
		names[a.Name] = true
	}

	for _, want := range []string{"directive", "unsafeflow", "lifetime", "sharing", "ownership"} {
		if !names[want] {
			t.Errorf("All() lacks %q", want)
		}
	}
}

func TestDefaultIsAll(t *testing.T) {
	all, def := analyzers.All(), analyzers.Default()
	if len(all) != len(def) {
		t.Fatalf("Default() has %d analyzers, All() has %d", len(def), len(all))
	}

	for i := range all {
		if all[i] != def[i] {
			t.Fatalf("Default()[%d] = %s, want %s", i, def[i].Name, all[i].Name)
		}
	}
}
