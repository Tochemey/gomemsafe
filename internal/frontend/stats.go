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

package frontend

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/tochemey/gomemsafe/internal/trace"
)

// stats aggregates the trace events of one run.
type stats struct {
	packages   map[string]bool
	byAnalyzer map[string]time.Duration
	total      time.Duration
	ssa        time.Duration
	budgetHits int
	maxRSS     int64
}

func printStats(p *printer, dir string) error {
	s, err := readStats(dir)
	if err != nil {
		return err
	}

	p.printf("\ngomemsafe stats: %d packages, %v in analyzers, peak RSS %.1f MiB\n",
		len(s.packages), s.total.Round(time.Millisecond), float64(s.maxRSS)/(1<<20))

	if s.total > 0 {
		p.printf("  SSA construction: %v (%.1f%% of analyzer time)\n",
			s.ssa.Round(time.Millisecond), 100*float64(s.ssa)/float64(s.total))
	}

	p.printf("  dataflow budget hits: %d\n", s.budgetHits)

	names := make([]string, 0, len(s.byAnalyzer))
	for n := range s.byAnalyzer {
		names = append(names, n)
	}

	sort.Slice(names, func(i, j int) bool { return s.byAnalyzer[names[i]] > s.byAnalyzer[names[j]] })

	for _, n := range names {
		p.printf("  %-12s %v\n", n, s.byAnalyzer[n].Round(time.Millisecond))
	}

	return nil
}

func readStats(dir string) (stats, error) {
	s := stats{packages: make(map[string]bool), byAnalyzer: make(map[string]time.Duration)}

	files, err := filepath.Glob(filepath.Join(dir, "*"+trace.FileExt))
	if err != nil {
		return s, err
	}

	for _, name := range files {
		if err := s.readFile(name); err != nil {
			return s, err
		}
	}

	return s, nil
}

func (s *stats) readFile(name string) error {
	f, err := os.Open(filepath.Clean(name))
	if err != nil {
		return err
	}
	// The file is only read, so a Close error cannot lose data.
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e trace.Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}

		s.add(e)
	}

	return sc.Err()
}

func (s *stats) add(e trace.Event) {
	s.packages[e.Package] = true
	s.maxRSS = max(s.maxRSS, e.MaxRSS)

	d := time.Duration(e.Nanos)
	switch e.Kind {
	case trace.KindRun:
		s.byAnalyzer[e.Analyzer] += d
		s.total += d
	case trace.KindSSA:
		s.ssa += d
	case trace.KindBudget:
		s.budgetHits++
	}
}
