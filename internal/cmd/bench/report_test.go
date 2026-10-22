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
	"strings"
	"testing"
	"time"
)

func TestVerdicts(t *testing.T) {
	m := measurement{
		ColdVet: 10 * time.Second, ColdTool: 15 * time.Second,
		WarmVet: time.Second, WarmTool: time.Second + 50*time.Millisecond,
		Edit: 250 * time.Millisecond, EditLines: 9_000,
		MaxRSSRatio: 1.5,
	}

	for name, got := range map[string]string{
		"cold": coldVerdict(m), "warm": warmVerdict(m), "edit": editVerdict(m), "rss": rssVerdict(m),
	} {
		if got != verdictMet {
			t.Errorf("%s: %s, want %s", name, got, verdictMet)
		}
	}

	m.ColdTool, m.WarmTool, m.Edit, m.MaxRSSRatio = 25*time.Second, 2*time.Second, 400*time.Millisecond, 2.5

	for name, got := range map[string]string{
		"cold": coldVerdict(m), "warm": warmVerdict(m), "edit": editVerdict(m), "rss": rssVerdict(m),
	} {
		if got != verdictMissed {
			t.Errorf("%s: %s, want %s", name, got, verdictMissed)
		}
	}

	if editVerdict(measurement{Edit: 500 * time.Millisecond, EditLines: 20_000}) != verdictMet {
		t.Error("the edit budget does not scale with package size")
	}

	empty := measurement{}
	for _, got := range []string{coldVerdict(empty), warmVerdict(empty), editVerdict(empty), rssVerdict(empty), ssaShare(empty), seconds(0)} {
		if got != verdictNA {
			t.Errorf("unmeasured: %s, want %s", got, verdictNA)
		}
	}
}

func TestMarkdownAndJSON(t *testing.T) {
	rep := report{Date: "2026-10-22", GoVersion: "go1.27.3", Machine: "m", Commit: "abc", Results: []result{{
		Entry: Entry{Name: "stdlib"},
		Measurement: measurement{Packages: 3, ColdVet: time.Second, ColdTool: time.Second, AnalyzerTime: 4,
			SSATime: 1, MaxRSSRatio: 0.5, RSSRatioPackage: "fmt", Edit: time.Second, EditPackage: "x", EditLines: 10},
	}}}

	rep.Results[0].Measurement.Findings = []finding{{Category: "MS301", Posn: "a.go:1:1", Message: "MS301: buf used"}}

	md := rep.markdown()
	for _, want := range []string{"### Findings on stdlib", "- `a.go:1:1`: MS301: buf used", "Go go1.27.3 on m, gomemsafe at abc", "| stdlib | 3 | 1.00s | 1.00s | met |", "25.0%", "0.50× (fmt)", "1.00s (x, 10, 0)"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}

	data, err := marshalReport(rep)
	if err != nil {
		t.Fatal(err)
	}

	var back report
	if err := json.Unmarshal(data, &back); err != nil || back.Results[0].Measurement.Packages != 3 {
		t.Fatalf("round trip: %+v, %v", back, err)
	}
}
