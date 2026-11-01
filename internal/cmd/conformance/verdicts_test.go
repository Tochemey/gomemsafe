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

package main

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/tochemey/gomemsafe/internal/frontend"
)

func TestExpectationsAreTheMarkedLines(t *testing.T) {
	src := "package p\n\nfunc f() {\n\tx = 1 // +checklocksfail\n\ty = 2\n\tz = 3 // +checklocksfail=invalid\n}\n"
	if got := expectations([]byte(src)); !slices.Equal(got, []int{4, 6}) {
		t.Fatalf("expectations = %v, want [4 6]", got)
	}

	if got := expectations(nil); got != nil {
		t.Fatalf("expectations of nothing = %v", got)
	}
}

// TestVerdictsKeepBreachesAndMalformedAnnotations checks which diagnostics count as a verdict:
// MS403 and a malformed checklocks annotation do; an unsupported annotation, another check, a
// //memsafe: directive, and an unparsable position do not; a line counts once.
func TestVerdictsKeepBreachesAndMalformedAnnotations(t *testing.T) {
	dir := filepath.Join("/", "corpus")
	posn := func(file string, line int) string { return filepath.Join(dir, file) + ":" + strconv.Itoa(line) + ":3" }

	diags := []frontend.Diagnostic{
		{Category: "MS403", Posn: posn("basics.go", 10), Message: "MS403: n written here"},
		{Category: "MS403", Posn: posn("basics.go", 10), Message: "MS403: m written here"},
		{Category: "MS403", Posn: posn("basics.go", 5), Message: "MS403: n read here"},
		{Category: "MS000", Posn: posn("return.go", 7), Message: "MS000: +checklocks:tc.mu does not resolve"},
		{Category: "MS000", Posn: posn("atomics.go", 3), Message: "MS000: +checkatomic is not supported yet by this version"},
		{Category: "MS000", Posn: posn("basics.go", 9), Message: "MS000: unknown directive //memsafe:alow"},
		{Category: "MS401", Posn: posn("basics.go", 11), Message: "MS401: x read here"},
		{Category: "MS403", Posn: "nowhere", Message: "MS403: lost"},
		{Category: "MS403", Posn: "file:x:1", Message: "MS403: lost"},
		{Category: "MS403", Posn: "file:1", Message: "MS403: lost"},
		{Category: "MS403", Posn: "outside.go:1:1", Message: "MS403: not under the corpus"},
	}

	got := verdicts(diags, dir)
	if len(got) != 2 || !slices.Equal(got["basics.go"], []int{5, 10}) || !slices.Equal(got["return.go"], []int{7}) {
		t.Fatalf("verdicts = %v", got)
	}
}

func TestCompareAndMarkdown(t *testing.T) {
	expected := map[string][]int{"basics.go": {5, 10}, "closures.go": {1, 2}, "anon.go": {4}}
	reported := map[string][]int{"basics.go": {5, 12}, "closures.go": {2, 3}, "anon.go": {4}}

	results := compare(expected, reported)
	if len(results) != len(corpusFiles) {
		t.Fatalf("%d results, want one per corpus file", len(results))
	}

	byName := make(map[string]fileResult)
	for _, r := range results {
		byName[r.file.name] = r
	}

	basics := byName["basics.go"]
	if basics.agrees() || !slices.Equal(basics.missing, []int{10}) || !slices.Equal(basics.extra, []int{12}) {
		t.Errorf("basics.go: %+v", basics)
	}

	if !byName["anon.go"].agrees() || !byName["defer.go"].agrees() {
		t.Error("anon.go or defer.go disagrees")
	}

	if got := disagreements(results); !slices.Equal(got, []string{"basics.go"}) {
		t.Errorf("disagreements = %v, want basics.go only: closures.go is context", got)
	}

	table := markdown(results)

	for _, want := range []string{
		"| basics.go | compared | 2 | 2 | 10 | 12 | DIFFER |",
		"| anon.go | compared | 1 | 1 | - | - | agree |",
		"| closures.go | context: values stored through inlined literals are not followed into fields | 2 | 2 | 1 | 3 | shown |",
		corpusVersion,
	} {
		if !strings.Contains(table, want) {
			t.Errorf("table lacks %q:\n%s", want, table)
		}
	}
}
