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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tochemey/gomemsafe/internal/frontend"
	"github.com/tochemey/gomemsafe/internal/trace"
)

// Environment variables the scenarios set for the go command and the tools.
const (
	envGoCache    = "GOCACHE"
	envTrace      = trace.EnvVar
	envCompileRSS = trace.CompileRSSEnvVar
	traceExt      = trace.FileExt
)

// The go command lines the scenarios run.
const (
	goVet          = "vet"
	goBuild        = "build"
	goList         = "list"
	vetToolFlag    = "-vettool="
	jsonFlag       = "-json"
	rebuildAllFlag = "-a"
	toolexecFlag   = "-toolexec="
	formatFlag     = "-f"
	// listFormat prints, per package: import path, directory, Go files, and imports.
	listFormat = "{{.ImportPath}}\t{{.Dir}}\t{{join .GoFiles \",\"}}\t{{join .Imports \",\"}}"
)

// Names inside one entry's scratch directory.
const (
	vetCacheDir     = "gocache-vet"
	toolCacheDir    = "gocache-gomemsafe"
	compileCacheDir = "gocache-compile"
	coldTraceDir    = "trace-cold"
	editTraceDir    = "trace-edit"
	compileRSSFile  = "compile-rss.tsv"
)

// vetFindingsExit is the status plain go vet exits with when it reports findings.
const vetFindingsExit = 1

// editComment is appended to the edited file: it changes the file, so the package is
// recompiled and re-analysed, without changing any declaration.
const editComment = "\n// bench: edited to measure incremental re-analysis.\n"

// Scenario names, as -scenario takes them.
const (
	scenarioCold = "cold"
	scenarioWarm = "warm"
	scenarioEdit = "edit"
	scenarioRSS  = "rss"
)

// allScenarios lists every scenario in the order they run. warm and edit reuse the cache
// that cold fills, and rss reads its trace, so selecting any of them also runs cold.
var allScenarios = []string{scenarioCold, scenarioWarm, scenarioEdit, scenarioRSS}

// tools are the binaries the scenarios run.
type tools struct {
	gomemsafe string
	rsswrap   string
}

// traceSummary aggregates the trace of one run.
type traceSummary struct {
	packages     map[string]bool
	rssByPackage map[string]int64
	// processTime is the longest gomemsafe process of each package.
	processTime  map[string]time.Duration
	analyzerTime time.Duration
	ssaTime      time.Duration
	budgetHits   int
}

// editTarget is the package the edit scenario modifies.
type editTarget struct {
	importPath string
	file       string
	lines      int
}

// finding is one gomemsafe diagnostic on the corpus, with its position relative to the
// analysed directory so that results are comparable across machines.
type finding struct {
	Category string `json:"category"`
	Posn     string `json:"posn"`
	Message  string `json:"message"`
}

// measurement is everything one corpus entry produced.
type measurement struct {
	Packages int           `json:"packages"`
	ColdVet  time.Duration `json:"cold_vet"`
	ColdTool time.Duration `json:"cold_gomemsafe"`
	WarmVet  time.Duration `json:"warm_vet"`
	WarmTool time.Duration `json:"warm_gomemsafe"`
	// Edit is the wall time of the whole go vet run after the edit; EditAnalysis is the
	// gomemsafe process of the edited package alone, which the edit budget applies to.
	Edit            time.Duration `json:"edit"`
	EditAnalysis    time.Duration `json:"edit_analysis"`
	EditPackage     string        `json:"edit_package,omitempty"`
	EditLines       int           `json:"edit_package_lines,omitempty"`
	EditReanalysed  int           `json:"edit_reanalysed_packages,omitempty"`
	AnalyzerTime    time.Duration `json:"analyzer_time"`
	SSATime         time.Duration `json:"ssa_time"`
	BudgetHits      int           `json:"budget_hits"`
	MaxToolRSS      int64         `json:"max_gomemsafe_rss"`
	MaxCompileRSS   int64         `json:"max_compile_rss"`
	MaxRSSRatio     float64       `json:"max_rss_ratio"`
	RSSRatioPackage string        `json:"max_rss_ratio_package,omitempty"`
	RSSRatioTool    int64         `json:"max_rss_ratio_gomemsafe,omitempty"`
	RSSRatioCompile int64         `json:"max_rss_ratio_compile,omitempty"`
	// Findings are gomemsafe's diagnostics from the cold run, to be triaged by hand.
	Findings []finding `json:"findings"`
}

// measure runs the selected scenarios on the entry whose source is in dir. work is a scratch
// directory for caches and traces.
func measure(ctx context.Context, dir string, e Entry, t tools, scenarios map[string]bool, work string) (measurement, error) {
	var m measurement

	vetCache := filepath.Join(work, vetCacheDir)
	toolCache := filepath.Join(work, toolCacheDir)

	vetArgs := append([]string{goVet}, e.Patterns...)
	toolArgs := append([]string{goVet, vetToolFlag + t.gomemsafe, jsonFlag}, e.Patterns...)

	// Every scenario needs the cold run: warm and edit runs start from its caches, and the RSS
	// comparison reads its trace.
	if scenarios[scenarioCold] || scenarios[scenarioWarm] || scenarios[scenarioEdit] || scenarios[scenarioRSS] {
		coldTrace := filepath.Join(work, coldTraceDir)

		var err error

		if m.ColdVet, err = timeVet(ctx, dir, vetArgs, vetCache); err != nil {
			return m, err
		}

		var output []byte

		if m.ColdTool, output, err = timeGo(ctx, dir, toolArgs, toolCache, coldTrace); err != nil {
			return m, err
		}

		if m.Findings, err = findings(dir, output); err != nil {
			return m, err
		}

		sum, err := readTrace(coldTrace)
		if err != nil {
			return m, err
		}

		m.Packages = len(sum.packages)
		m.AnalyzerTime, m.SSATime, m.BudgetHits = sum.analyzerTime, sum.ssaTime, sum.budgetHits
		m.MaxToolRSS = maxValue(sum.rssByPackage)

		if scenarios[scenarioRSS] {
			if err := measureRSS(ctx, dir, e, t, work, sum.rssByPackage, &m); err != nil {
				return m, err
			}
		}
	}

	if scenarios[scenarioWarm] {
		var err error

		if m.WarmVet, err = timeVet(ctx, dir, vetArgs, vetCache); err != nil {
			return m, err
		}

		if m.WarmTool, _, err = timeGo(ctx, dir, toolArgs, toolCache, ""); err != nil {
			return m, err
		}
	}

	if scenarios[scenarioEdit] {
		if err := measureEdit(ctx, dir, e, toolArgs, toolCache, work, &m); err != nil {
			return m, err
		}
	}

	return m, nil
}

// measureEdit appends a comment to one file of the most-imported package, re-runs gomemsafe,
// and restores the file.
func measureEdit(ctx context.Context, dir string, e Entry, toolArgs []string, cache, work string, m *measurement) error {
	target, err := pickEditTarget(ctx, dir, e.Patterns)
	if err != nil {
		return err
	}

	// The edit is confined to the package's directory.
	pkgDir, err := os.OpenRoot(filepath.Dir(target.file))
	if err != nil {
		return err
	}
	defer func() { _ = pkgDir.Close() }()

	name := filepath.Base(target.file)

	original, err := pkgDir.ReadFile(name)
	if err != nil {
		return err
	}

	defer func() { _ = pkgDir.WriteFile(name, original, 0o600) }()

	if err := pkgDir.WriteFile(name, append(bytes.Clone(original), editComment...), 0o600); err != nil {
		return err
	}

	editTrace := filepath.Join(work, editTraceDir)

	if m.Edit, _, err = timeGo(ctx, dir, toolArgs, cache, editTrace); err != nil {
		return err
	}

	sum, err := readTrace(editTrace)
	if err != nil {
		return err
	}

	m.EditPackage, m.EditLines, m.EditReanalysed = target.importPath, target.lines, len(sum.packages)
	m.EditAnalysis = sum.processTime[target.importPath]

	return nil
}

// measureRSS compiles every package from scratch through rsswrap and compares each compile's
// peak RSS with gomemsafe's for the same package.
func measureRSS(ctx context.Context, dir string, e Entry, t tools, work string, toolRSS map[string]int64, m *measurement) error {
	out := filepath.Join(work, compileRSSFile)

	args := append([]string{goBuild, rebuildAllFlag, toolexecFlag + t.rsswrap}, e.Patterns...)

	cmd := goCommand(ctx, dir, args, filepath.Join(work, compileCacheDir), "")
	cmd.Env = append(cmd.Env, envCompileRSS+"="+out)

	if _, err := run(cmd); err != nil {
		return err
	}

	compileRSS, err := readRSS(out)
	if err != nil {
		return err
	}

	m.MaxCompileRSS = maxValue(compileRSS)
	m.MaxRSSRatio, m.RSSRatioPackage = maxRatio(toolRSS, compileRSS)
	m.RSSRatioTool, m.RSSRatioCompile = toolRSS[m.RSSRatioPackage], compileRSS[m.RSSRatioPackage]

	return nil
}

// pickEditTarget returns the package imported by the most other packages matched by
// patterns, which is where an edit costs the most, and its first Go file. Ties go to the
// lexically first import path, so the choice is deterministic.
func pickEditTarget(ctx context.Context, dir string, patterns []string) (editTarget, error) {
	cmd := goCommand(ctx, dir, append([]string{goList, formatFlag, listFormat}, patterns...), "", "")

	out, err := run(cmd)
	if err != nil {
		return editTarget{}, err
	}

	target, err := mostImported(out)
	if err != nil {
		return editTarget{}, err
	}

	target.lines, err = countLines(filepath.Dir(target.file))

	return target, err
}

// mostImported parses `go list` lines of import path, directory, Go files, and imports.
func mostImported(listing []byte) (editTarget, error) {
	type pkg struct{ dir, firstFile string }

	pkgs := make(map[string]pkg)
	importers := make(map[string]int)

	for line := range strings.Lines(string(listing)) {
		f := strings.Split(strings.TrimRight(line, "\n"), "\t")
		if len(f) != 4 || f[2] == "" {
			continue
		}

		files := strings.Split(f[2], ",")
		slices.Sort(files)
		pkgs[f[0]] = pkg{dir: f[1], firstFile: files[0]}

		for imp := range strings.SplitSeq(f[3], ",") {
			importers[imp]++
		}
	}

	best := ""

	for path := range pkgs {
		if best == "" || importers[path] > importers[best] || (importers[path] == importers[best] && path < best) {
			best = path
		}
	}

	if best == "" {
		return editTarget{}, fmt.Errorf("no package with Go files to edit")
	}

	return editTarget{importPath: best, file: filepath.Join(pkgs[best].dir, pkgs[best].firstFile)}, nil
}

// countLines returns the number of lines in the non-test Go files of dir.
func countLines(dir string) (int, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return 0, err
	}

	lines := 0

	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}

		data, err := os.ReadFile(filepath.Clean(f))
		if err != nil {
			return 0, err
		}

		lines += bytes.Count(data, []byte("\n"))
	}

	return lines, nil
}

// readTrace aggregates every trace file in dir. A missing directory means nothing ran.
func readTrace(dir string) (traceSummary, error) {
	sum := traceSummary{
		packages: make(map[string]bool), rssByPackage: make(map[string]int64),
		processTime: make(map[string]time.Duration),
	}

	files, err := filepath.Glob(filepath.Join(dir, "*"+traceExt))
	if err != nil {
		return sum, err
	}

	for _, name := range files {
		if err := sum.readFile(name); err != nil {
			return sum, err
		}
	}

	return sum, nil
}

// readRSS reads rsswrap's output: one "package<TAB>bytes" line per compile.
func readRSS(path string) (map[string]int64, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	rss := make(map[string]int64)

	for line := range strings.Lines(string(data)) {
		pkg, value, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok {
			continue
		}

		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s: %q: %w", path, line, err)
		}

		rss[pkg] = max(rss[pkg], n)
	}

	return rss, nil
}

// maxRatio returns the largest tool/compile RSS ratio over the packages both measured.
func maxRatio(tool, compile map[string]int64) (float64, string) {
	best, bestPkg := 0.0, ""

	for pkg, t := range tool {
		c, ok := compile[pkg]
		if !ok || c == 0 {
			continue
		}

		if r := float64(t) / float64(c); r > best || (r == best && pkg < bestPkg) {
			best, bestPkg = r, pkg
		}
	}

	return best, bestPkg
}

func maxValue(m map[string]int64) int64 {
	var best int64
	for _, v := range m {
		best = max(best, v)
	}

	return best
}

// timeGo runs the go command and returns its wall time and stdout. Any failure is an error:
// gomemsafe with -json exits 0 even when it has findings, so a non-zero exit is a build
// failure or a crash, and its time would not be a measurement.
func timeGo(ctx context.Context, dir string, args []string, cache, traceDir string) (time.Duration, []byte, error) {
	if traceDir != "" {
		if err := os.MkdirAll(traceDir, 0o750); err != nil {
			return 0, nil, err
		}
	}

	cmd := goCommand(ctx, dir, args, cache, traceDir)
	start := time.Now()

	out, err := run(cmd)
	if err != nil {
		return 0, nil, err
	}

	return time.Since(start), out, nil
}

// timeVet runs plain go vet and returns its wall time. Plain go vet exits 1 when it reports
// findings; that is a result, not a failure.
func timeVet(ctx context.Context, dir string, args []string, cache string) (time.Duration, error) {
	cmd := goCommand(ctx, dir, args, cache, "")
	start := time.Now()

	_, err := run(cmd)

	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || exit.ExitCode() != vetFindingsExit) {
		return 0, err
	}

	return time.Since(start), nil
}

// findings decodes gomemsafe's JSON output, with positions made relative to dir.
func findings(dir string, output []byte) ([]finding, error) {
	diags, _, err := frontend.Decode(output)
	if err != nil {
		return nil, err
	}

	out := make([]finding, 0, len(diags))

	for _, d := range diags {
		posn := d.Posn
		if rel, err := filepath.Rel(dir, posn); err == nil && filepath.IsLocal(rel) {
			posn = rel
		}

		out = append(out, finding{Category: d.Category, Posn: posn, Message: d.Message})
	}

	return out, nil
}

// goCommand prepares a go command in dir with its own build cache and, optionally, a trace
// directory. An empty cache keeps the caller's. Cancelling ctx kills the command and every
// process it started.
func goCommand(ctx context.Context, dir string, args []string, cache, traceDir string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "go", args...) //nolint:gosec // G204: corpus patterns and pinned tool paths
	cmd.Dir = dir
	stopGroupOnCancel(cmd)

	cmd.Env = os.Environ()
	if cache != "" {
		cmd.Env = append(cmd.Env, envGoCache+"="+cache)
	}

	cmd.Env = append(cmd.Env, envTrace+"="+traceDir)

	return cmd
}

// run runs cmd and returns its stdout; stderr is included in the error.
func run(cmd *exec.Cmd) ([]byte, error) {
	var stdout, stderr bytes.Buffer

	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("%s: %w\n%s", strings.Join(cmd.Args, " "), err, lastLines(stderr.String(), 20))
	}

	return stdout.Bytes(), nil
}

// lastLines keeps the end of a long error output.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	return strings.Join(lines, "\n")
}

func (s *traceSummary) readFile(name string) error {
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

func (s *traceSummary) add(e trace.Event) {
	s.packages[e.Package] = true
	s.rssByPackage[e.Package] = max(s.rssByPackage[e.Package], e.MaxRSS)
	s.processTime[e.Package] = max(s.processTime[e.Package], time.Duration(e.Elapsed))

	switch e.Kind {
	case trace.KindRun:
		s.analyzerTime += time.Duration(e.Nanos)
	case trace.KindSSA:
		s.ssaTime += time.Duration(e.Nanos)
	case trace.KindBudget:
		s.budgetHits++
	}
}
