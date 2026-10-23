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

// Command bench measures gomemsafe against its performance budgets on a small pinned corpus
// (corpus.json): two real libraries, zap and GoAkt, each pinned by commit, and a generated
// corpus that exercises every part of the engine. The corpus stays small enough to run on
// every CI build.
//
//	go run ./internal/cmd/bench [-short] [-corpus zap,goakt,synthetic] [-scenario cold,warm,edit,rss] [-out dir]
//
// For each corpus entry it measures a cold go vet against a cold gomemsafe run (each with a
// fresh build cache), the warm re-runs of both, an edit to the most-imported package, and the
// peak RSS of every gomemsafe process against the compile of the same package. The results
// table is printed; with -out, it is also written as JSON and markdown to that directory.
//
// A run leaves nothing behind. Repositories are cloned, at their pinned commits, into the
// run's temporary directory together with its build caches and traces, and the directory is
// removed when the run ends, including when it is interrupted: Ctrl-C or SIGTERM kills every
// go command the run started, with all of its children, before the cleanup.
//
// -short runs only a small generated corpus, which needs no network.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"
)

// Locations, relative to the repository root, and inside the run's temporary directory.
const (
	benchDir         = "internal/cmd/bench"
	corpusFileName   = "corpus.json"
	gomemsafePackage = "./cmd/gomemsafe"
	rsswrapPackage   = "./internal/cmd/rsswrap"
	workPattern      = "gomemsafe-bench-"
	clonesDir        = "clones"
	dateLayout       = "2006-01-02"
)

// The git arguments the benchmark uses.
const (
	gitRevParse  = "rev-parse"
	gitHead      = "HEAD"
	gitTopLevel  = "--show-toplevel"
	gitShort     = "--short"
	gitQuiet     = "--quiet"
	gitRemote    = "origin"
	gitFetchHead = "FETCH_HEAD"
)

// options is the parsed command line.
type options struct {
	corpusFile string
	names      []string
	short      bool
	scenarios  map[string]bool
	outDir     string
}

func main() {
	os.Exit(runBench(os.Args[1:], os.Stdout, os.Stderr))
}

func runBench(args []string, stdout, stderr io.Writer) int {
	o, err := parseOptions(args, stderr)
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(stderr, "bench:", err)
		}

		return 2
	}

	// An interrupt cancels the run; benchmark then cleans up before returning.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := benchmark(ctx, o, stdout, stderr); err != nil {
		_, _ = fmt.Fprintln(stderr, "bench:", err)
		return 1
	}

	return 0
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	fs.SetOutput(stderr)

	o := options{}
	fs.StringVar(&o.corpusFile, "corpus-file", "", "corpus definition (default "+benchDir+"/"+corpusFileName+")")
	names := fs.String("corpus", "", "comma-separated corpus entries to run (default: all)")
	fs.BoolVar(&o.short, "short", false, "run only a small generated corpus, which needs no network")
	scenarios := fs.String("scenario", strings.Join(allScenarios, ","), "comma-separated scenarios")
	fs.StringVar(&o.outDir, "out", "", "directory to also write the JSON and markdown results to (default: print only)")

	if err := fs.Parse(args); err != nil {
		return o, err
	}

	if *names != "" {
		o.names = strings.Split(*names, ",")
	}

	o.scenarios = make(map[string]bool)

	for s := range strings.SplitSeq(*scenarios, ",") {
		if !slices.Contains(allScenarios, s) {
			return o, fmt.Errorf("unknown scenario %q (want %s)", s, strings.Join(allScenarios, ", "))
		}

		o.scenarios[s] = true
	}

	return o, nil
}

// benchmark runs every selected entry and reports the results. Everything it creates lives in
// one temporary directory, removed when it returns, whether it succeeds, fails, or ctx is
// cancelled.
func benchmark(ctx context.Context, o options, stdout, stderr io.Writer) error {
	root, err := gitOutput(ctx, "", gitRevParse, gitTopLevel)
	if err != nil {
		return err
	}

	if o.corpusFile == "" {
		o.corpusFile = filepath.Join(root, benchDir, corpusFileName)
	}

	all, err := loadCorpus(o.corpusFile)
	if err != nil {
		return err
	}

	entries, err := selectEntries(all, o.names, o.short)
	if err != nil {
		return err
	}

	work, err := os.MkdirTemp("", workPattern)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()

	t, err := buildTools(ctx, root, work)
	if err != nil {
		return err
	}

	rep := newReport(ctx, root)

	for _, e := range entries {
		_, _ = fmt.Fprintf(stderr, "bench: %s\n", e.Name)

		dir, err := prepareSource(ctx, e, work)
		if err != nil {
			return fmt.Errorf("%s: %w", e.Name, err)
		}

		entryWork := filepath.Join(work, e.Name)

		m, err := measure(ctx, dir, e, t, o.scenarios, entryWork)
		if ctx.Err() != nil {
			return fmt.Errorf("%s: interrupted: %w", e.Name, ctx.Err())
		}

		if err != nil {
			return fmt.Errorf("%s: %w", e.Name, err)
		}

		rep.Results = append(rep.Results, result{Entry: e, Measurement: m})
	}

	return writeReport(rep, o.outDir, stdout)
}

func newReport(ctx context.Context, root string) report {
	commit, err := gitOutput(ctx, root, gitRevParse, gitShort, gitHead)
	if err != nil {
		commit = "unknown"
	}

	if status, err := gitOutput(ctx, root, "status", "--porcelain"); err == nil && status != "" {
		commit += "+uncommitted"
	}

	return report{
		Date:      time.Now().UTC().Format(dateLayout),
		GoVersion: runtime.Version(),
		Machine:   fmt.Sprintf("%s/%s, %d CPUs", runtime.GOOS, runtime.GOARCH, runtime.NumCPU()),
		Commit:    commit,
	}
}

// buildTools builds gomemsafe and rsswrap from the repository.
func buildTools(ctx context.Context, root, work string) (tools, error) {
	t := tools{gomemsafe: filepath.Join(work, "gomemsafe"), rsswrap: filepath.Join(work, "rsswrap")}

	builds := []struct{ dir, out, pkg string }{
		{root, t.gomemsafe, gomemsafePackage},
		{root, t.rsswrap, rsswrapPackage},
	}

	for _, b := range builds {
		if _, err := run(goCommand(ctx, b.dir, []string{"build", "-o", b.out, b.pkg}, "", "")); err != nil {
			return t, err
		}
	}

	return t, nil
}

// prepareSource returns the directory to analyse for e: the generated corpus or a clone at
// the pinned commit, both inside work.
func prepareSource(ctx context.Context, e Entry, work string) (string, error) {
	if e.Synthetic {
		return generateSynthetic(filepath.Join(work, syntheticEntry), e.Small)
	}

	dir, err := cloneAt(ctx, e, filepath.Join(work, clonesDir))
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, e.Dir), nil
}

// cloneAt clones e's repository at its pinned commit into a fresh directory under parent.
func cloneAt(ctx context.Context, e Entry, parent string) (string, error) {
	dir := filepath.Join(parent, e.cloneName())
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}

	steps := [][]string{
		{"init", gitQuiet},
		{"remote", "add", gitRemote, e.Repo},
		{"fetch", gitQuiet, "--depth", "1", gitRemote, e.Commit},
		{"checkout", gitQuiet, "--detach", gitFetchHead},
	}

	for _, args := range steps {
		if _, err := gitOutput(ctx, dir, args...); err != nil {
			return "", err
		}
	}

	head, err := gitOutput(ctx, dir, gitRevParse, gitHead)
	if err != nil {
		return "", err
	}

	if head != e.Commit {
		return "", fmt.Errorf("clone of %s is at %s, want the pinned %s", e.Repo, head, e.Commit)
	}

	return dir, nil
}

// writeReport prints the markdown and, when dir is set, also writes <date>-<commit>.json and
// .md there.
func writeReport(rep report, dir string, stdout io.Writer) error {
	md := rep.markdown()

	if dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}

		data, err := marshalReport(rep)
		if err != nil {
			return err
		}

		base := filepath.Join(dir, rep.Date+"-"+rep.Commit)

		if err := os.WriteFile(base+".json", data, 0o600); err != nil {
			return err
		}

		if err := os.WriteFile(base+".md", []byte(md), 0o600); err != nil {
			return err
		}
	}

	_, err := fmt.Fprint(stdout, md)

	return err
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	return commandOutput(ctx, dir, "git", args...)
}

// commandOutput runs a fixed program (git or go) and returns its trimmed stdout. Cancelling
// ctx kills it and every process it started.
func commandOutput(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: git and go with bench-chosen arguments
	cmd.Dir = dir
	stopGroupOnCancel(cmd)

	out, err := run(cmd)

	return strings.TrimSpace(string(out)), err
}
