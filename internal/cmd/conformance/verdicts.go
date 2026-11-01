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
	"bytes"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/tochemey/gomemsafe/internal/catalog"
	"github.com/tochemey/gomemsafe/internal/frontend"
)

// The markers in the corpus and in gomemsafe's output that the comparison reads.
const (
	failMarker     = "+checklocksfail"
	annotationMark = "+check"
	unsupported    = "is not supported yet"
)

// corpusFile is one file of checklocks' test package. A compared file must agree line for
// line; a context file uses annotations gomemsafe does not check yet, or behaviour it does
// not model, and its agreement is shown only.
type corpusFile struct {
	name     string
	compared bool
	// reason says why a context file is not compared.
	reason string
}

// fileResult is the comparison of one file: the lines checklocks marks as failing, the lines
// gomemsafe reports, and the differences.
type fileResult struct {
	file           corpusFile
	expected       int
	reported       int
	missing, extra []int
}

// corpusFiles lists the test package's files, in the order the table shows them.
var corpusFiles = []corpusFile{
	{name: "anon.go", compared: true},
	{name: "basics.go", compared: true},
	{name: "branches.go", compared: true},
	{name: "cyclic.go", compared: true},
	{name: "defer.go", compared: true},
	{name: "generics.go", compared: true},
	{name: "locker.go", compared: true},
	{name: "methods.go", compared: true},
	{name: "parameters.go", compared: true},
	{name: "private_globals.go", compared: true},
	{name: "return.go", compared: true},
	{name: "test.go", compared: true},
	{name: "alignment.go", reason: "alignment checking is checkalign's, not MS403's"},
	{name: "aliases.go", reason: "+checklocksalias is not supported yet"},
	{name: "atomics.go", reason: "+checkatomic is not supported yet"},
	{name: "closures.go", reason: "values stored through inlined literals are not followed into fields"},
	{name: "declarations.go", reason: "+checkatomic is not supported yet"},
	{name: "globals.go", reason: "+checkatomic is not supported yet"},
	{name: "incompat.go", reason: "+checkatomic is not supported yet"},
	{name: "inferred.go", reason: "guard inference is not done"},
	{name: "rwmutex.go", reason: "+checklocksreadany and +checklocksexcludewrite are not supported yet"},
	{name: filepath.Join("crosspkg", "crosspkg.go"), reason: "+checkatomic is not supported yet"},
	{name: filepath.Join("atomic", "atomic.go"), reason: "+checkatomic is not supported yet"},
}

// agrees reports whether gomemsafe reported exactly the lines checklocks marks.
func (r fileResult) agrees() bool { return len(r.missing) == 0 && len(r.extra) == 0 }

// expectations returns the lines of src that carry checklocks' failure marker, from 1.
func expectations(src []byte) []int {
	var lines []int

	for i, line := range bytes.Split(src, []byte("\n")) {
		if bytes.Contains(line, []byte(failMarker)) {
			lines = append(lines, i+1)
		}
	}

	return lines
}

// verdicts returns, by file relative to dir, the lines where diags say a lock contract is
// broken: MS403, and MS000 for a malformed checklocks annotation, which checklocks reports
// too. MS000 for an annotation that is not supported yet is neutral, as is everything else.
func verdicts(diags []frontend.Diagnostic, dir string) map[string][]int {
	reported := make(map[string][]int)

	for _, d := range diags {
		switch d.Category {
		case catalog.UnguardedField.String():
		case catalog.MalformedDirective.String():
			if !strings.Contains(d.Message, annotationMark) || strings.Contains(d.Message, unsupported) {
				continue
			}
		default:
			continue
		}

		file, line, ok := splitPosn(d.Posn)
		if !ok {
			continue
		}

		rel, err := filepath.Rel(dir, file)
		if err != nil {
			continue
		}

		if !slices.Contains(reported[rel], line) {
			reported[rel] = append(reported[rel], line)
		}
	}

	for _, lines := range reported {
		slices.Sort(lines)
	}

	return reported
}

// splitPosn splits a file:line:column position.
func splitPosn(posn string) (string, int, bool) {
	i := strings.LastIndex(posn, ":")
	if i < 0 {
		return "", 0, false
	}

	j := strings.LastIndex(posn[:i], ":")
	if j < 0 {
		return "", 0, false
	}

	line, err := strconv.Atoi(posn[j+1 : i])
	if err != nil {
		return "", 0, false
	}

	return posn[:j], line, true
}

// compare matches the expected lines of every corpus file with the reported ones.
func compare(expected, reported map[string][]int) []fileResult {
	results := make([]fileResult, len(corpusFiles))

	for i, f := range corpusFiles {
		want, got := expected[f.name], reported[f.name]
		results[i] = fileResult{file: f, expected: len(want), reported: len(got),
			missing: difference(want, got), extra: difference(got, want)}
	}

	return results
}

// difference returns the elements of a not in b, in order.
func difference(a, b []int) []int {
	var out []int

	for _, x := range a {
		if !slices.Contains(b, x) {
			out = append(out, x)
		}
	}

	return out
}

// disagreements names the compared files whose verdicts differ.
func disagreements(results []fileResult) []string {
	var names []string

	for _, r := range results {
		if r.file.compared && !r.agrees() {
			names = append(names, r.file.name)
		}
	}

	return names
}

// markdown renders the results as a table: one row per file, with the lines where the
// verdicts differ.
func markdown(results []fileResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## checklocks conformance (gVisor %s)\n\n", corpusVersion)
	b.WriteString("| File | Role | checklocks fails | gomemsafe reports | Missing | Extra | Verdict |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")

	for _, r := range results {
		role, verdict := "context: "+r.file.reason, "shown"
		if r.file.compared {
			role, verdict = "compared", "agree"
			if !r.agrees() {
				verdict = "DIFFER"
			}
		}

		fmt.Fprintf(&b, "| %s | %s | %d | %d | %s | %s | %s |\n", r.file.name, role, r.expected, r.reported,
			lines(r.missing), lines(r.extra), verdict)
	}

	return b.String()
}

// lines words a list of line numbers, or a dash for none.
func lines(nums []int) string {
	if len(nums) == 0 {
		return "-"
	}

	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = strconv.Itoa(n)
	}

	return strings.Join(parts, " ")
}
