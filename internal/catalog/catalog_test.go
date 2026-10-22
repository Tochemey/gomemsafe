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

package catalog

import "testing"

func TestIsKnown(t *testing.T) {
	for _, id := range []string{"MS000", "MS101", "MS301", "MS503", "MS903"} {
		if !IsKnown(id) {
			t.Errorf("IsKnown(%q) = false", id)
		}
	}

	for _, id := range []string{"MS999", "MS3011", "ms301", ""} {
		if IsKnown(id) {
			t.Errorf("IsKnown(%q) = true", id)
		}
	}
}

func TestKnownHasNoDuplicates(t *testing.T) {
	seen := make(map[ID]bool)

	for _, id := range known {
		if seen[id] {
			t.Fatalf("%s is listed twice", id)
		}

		seen[id] = true
	}
}

func TestKnownIsInCatalogueOrder(t *testing.T) {
	for i := 1; i < len(known); i++ {
		if known[i-1] >= known[i] {
			t.Fatalf("%s is listed before %s", known[i-1], known[i])
		}
	}
}

func TestString(t *testing.T) {
	if UseAfterInvalidation.String() != "MS301" {
		t.Fatalf("String() = %q", UseAfterInvalidation.String())
	}
}
