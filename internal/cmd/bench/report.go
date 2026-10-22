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
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Budgets of docs/design.md §7.1.
const (
	// maxColdRatio is the cold-run budget: gomemsafe within twice go vet's time.
	maxColdRatio = 2.0
	// editBudgetPer10kLines is the incremental budget for an edit to a 10 kLOC package.
	editBudgetPer10kLines = 300 * time.Millisecond
	// maxRSSRatio is the memory budget: each package within twice its compile's peak RSS.
	maxRSSRatio = 2.0
	// warmSlack absorbs timer noise when comparing two cache-hit runs.
	warmSlack = 100 * time.Millisecond
	// bytesPerMB converts peak RSS for the report.
	bytesPerMB = 1 << 20
)

// Verdicts, as the report prints them.
const (
	verdictMet    = "met"
	verdictMissed = "missed"
	verdictNA     = "n/a"
)

// result is one corpus entry's measurement with the context needed to reproduce it.
type result struct {
	Entry       Entry       `json:"entry"`
	Measurement measurement `json:"measurement"`
}

// report is a whole run.
type report struct {
	Date      string   `json:"date"`
	GoVersion string   `json:"go_version"`
	Machine   string   `json:"machine"`
	Commit    string   `json:"gomemsafe_commit"`
	Results   []result `json:"results"`
}

// markdown renders the report as a table, followed by the findings to triage.
func (r report) markdown() string {
	var b strings.Builder

	fmt.Fprintf(&b, "Go %s on %s, gomemsafe at %s, %s.\n\n", r.GoVersion, r.Machine, r.Commit, r.Date)
	b.WriteString("| Corpus | Packages | Cold vet | Cold gomemsafe | Cold ≤ 2× | Warm vet | Warm gomemsafe | Warm ≤ vet | Edit: analysis of wall (package, lines, re-analysed) | Edit budget | Peak RSS vs compile (package: gomemsafe vs compile) | RSS ≤ 2× | SSA share of analysis | Budget hits | Findings |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")

	for _, res := range r.Results {
		m := res.Measurement
		fmt.Fprintf(&b, "| %s | %d | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %d | %d |\n",
			res.Entry.Name, m.Packages, seconds(m.ColdVet), seconds(m.ColdTool), coldVerdict(m),
			seconds(m.WarmVet), seconds(m.WarmTool), warmVerdict(m),
			editCell(m), editVerdict(m), rssCell(m), rssVerdict(m), ssaShare(m), m.BudgetHits, len(m.Findings))
	}

	for _, res := range r.Results {
		if len(res.Measurement.Findings) == 0 {
			continue
		}

		fmt.Fprintf(&b, "\n### Findings on %s\n\n", res.Entry.Name)

		for _, f := range res.Measurement.Findings {
			fmt.Fprintf(&b, "- `%s`: %s\n", f.Posn, f.Message)
		}
	}

	return b.String()
}

func coldVerdict(m measurement) string {
	if m.ColdVet == 0 || m.ColdTool == 0 {
		return verdictNA
	}

	return verdict(float64(m.ColdTool)/float64(m.ColdVet) <= maxColdRatio)
}

func warmVerdict(m measurement) string {
	if m.WarmVet == 0 || m.WarmTool == 0 {
		return verdictNA
	}

	return verdict(m.WarmTool <= m.WarmVet+warmSlack)
}

// editVerdict judges the re-analysis of the edited package, not the whole go vet run, whose
// wall time also covers the go command's own work and the package's recompilation. The 10 kLOC
// budget scales with the package's size, never below the budget itself.
func editVerdict(m measurement) string {
	if m.EditAnalysis == 0 {
		return verdictNA
	}

	budget := editBudgetPer10kLines * time.Duration(max(1, (m.EditLines+9_999)/10_000))

	return verdict(m.EditAnalysis <= budget)
}

func rssVerdict(m measurement) string {
	if m.MaxRSSRatio == 0 {
		return verdictNA
	}

	return verdict(m.MaxRSSRatio <= maxRSSRatio)
}

func verdict(ok bool) string {
	if ok {
		return verdictMet
	}

	return verdictMissed
}

func editCell(m measurement) string {
	if m.Edit == 0 {
		return verdictNA
	}

	return fmt.Sprintf("%s of %s (%s, %d, %d)", seconds(m.EditAnalysis), seconds(m.Edit), m.EditPackage,
		m.EditLines, m.EditReanalysed)
}

func rssCell(m measurement) string {
	if m.MaxRSSRatio == 0 {
		return verdictNA
	}

	return fmt.Sprintf("%.2f× (%s: %d MB vs %d MB)", m.MaxRSSRatio, m.RSSRatioPackage,
		m.RSSRatioTool/bytesPerMB, m.RSSRatioCompile/bytesPerMB)
}

func ssaShare(m measurement) string {
	if m.AnalyzerTime == 0 {
		return verdictNA
	}

	return fmt.Sprintf("%.1f%%", 100*float64(m.SSATime)/float64(m.AnalyzerTime))
}

func seconds(d time.Duration) string {
	if d == 0 {
		return verdictNA
	}

	return fmt.Sprintf("%.2fs", d.Seconds())
}

// marshalReport encodes the report as indented JSON, with durations in nanoseconds.
func marshalReport(rep report) ([]byte, error) {
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return nil, err
	}

	return append(data, '\n'), nil
}
