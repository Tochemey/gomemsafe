# Check commands for gomemsafe. CONSTRAINTS.md is the source of truth for every rule and
# number; this file mirrors it. Tools run with `go run pkg@version`: nothing is installed.
# Each version carries a renovate comment, so Renovate keeps it on the latest release.

SHELL := /bin/bash
# GNU make before 3.82, as shipped with macOS, ignores .SHELLFLAGS: recipes must not rely on
# these flags to fail, and check every command whose failure matters explicitly.
.SHELLFLAGS := -o pipefail -ec

# renovate: datasource=go depName=github.com/golangci/golangci-lint/v2
GOLANGCI_LINT_VERSION := v2.15.0
# renovate: datasource=go depName=golang.org/x/vuln
GOVULNCHECK_VERSION := v1.9.0
# renovate: datasource=go depName=github.com/zricethezav/gitleaks/v8
GITLEAKS_VERSION := v8.30.1
# renovate: datasource=go depName=github.com/rhysd/actionlint
ACTIONLINT_VERSION := v1.7.12

GOLANGCI_LINT := github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
GOVULNCHECK   := golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
GITLEAKS      := github.com/zricethezav/gitleaks/v8@$(GITLEAKS_VERSION)
ACTIONLINT    := github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)
COVERAGE_MIN  := 85
COVER_PROFILE := coverage.out
# BENCH_OUT, when set, also writes the benchmark results as JSON and markdown to that directory.
BENCH_OUT     ?=
# FUZZTIME is how long make fuzz runs each fuzz target.
FUZZTIME      ?= 30s

.PHONY: check-fast check-task check-full vet lint deps check-tests secrets test vuln actionlint bench bench-short fuzz clean

## check-fast: the edit loop (seconds)
check-fast: vet lint deps check-tests secrets

## check-task: before handing a task over for review (at most 90 seconds)
check-task: check-fast test

## check-full: CI
check-full: check-task vuln actionlint

vet:
	go vet ./...

lint:
	go run $(GOLANGCI_LINT) run ./...

# F2: every module in the build must be on the allowlist in CONSTRAINTS.md. -mod=mod because
# `go list -m all` cannot run in vendor mode. The module list is captured on its own, so that a
# failing go list stops the target; grep's status 1 only means that nothing is off the list.
deps:
	@allowed=$$(awk '/^## Dependency allowlist/{s=1} s && /^```/{n++; next} s && n==1' CONSTRAINTS.md) || exit 1; \
	mods=$$(go list -mod=mod -m -f '{{.Path}}' all) || exit 1; \
	bad=$$(grep -vxF -f <(printf '%s\n' "$$allowed") <<<"$$mods") || [ $$? -eq 1 ] || exit 1; \
	if [ -n "$$bad" ]; then echo "modules outside the CONSTRAINTS.md allowlist:" >&2; echo "$$bad" >&2; exit 1; fi; \
	echo "deps: all modules allowlisted"

# F8: x.go and x_test.go come in pairs. Only the repository's own files count: git ls-files
# honours .gitignore (vendor/), and testdata/ holds fixtures, not code. The file list is
# captured on its own, so that a failing git stops the target instead of checking nothing.
check-tests:
	@files=$$(git ls-files --cached --others --exclude-standard -- '*.go') || exit 1; \
	[ -n "$$files" ] || { echo "check-tests: no Go files found" >&2; exit 1; }; \
	bad=0; \
	while IFS= read -r f; do \
	  case "$$f" in \
	    testdata/*|*/testdata/*) ;; \
	    *_test.go) [ -f "$${f%_test.go}.go" ] || { echo "test file without implementation: $$f" >&2; bad=1; } ;; \
	    *.go)      [ -f "$${f%.go}_test.go" ] || { echo "implementation without unit test: $$f" >&2; bad=1; } ;; \
	  esac; \
	done <<<"$$files"; \
	if [ $$bad -ne 0 ]; then exit 1; fi; \
	echo "check-tests: every file has its test"

# F5: no secrets; findings are always redacted. .gitleaks.toml keeps the default rules and
# skips the gitignored vendor/ directory.
secrets:
	go run $(GITLEAKS) dir --redact --no-banner --config .gitleaks.toml .

# Tests with the race detector, then >= COVERAGE_MIN% statement coverage per package in
# analyzers/ and internal/. Coverage comes from one profile over all tests (-coverpkg), so code
# exercised by another package's tests counts; blocks repeated across test binaries are merged.
# A profile that measures no package fails, so that a wrong module path cannot pass the gate.
test:
	go test -race -covermode=atomic -coverpkg=./... -coverprofile=$(COVER_PROFILE) ./...
	@awk -v min=$(COVERAGE_MIN) -v mod="$$(go list -m)" ' \
	  NR == 1 { next } \
	  { split($$1, loc, ":"); file = loc[1]; key = $$1; stmts[key] = $$2; if ($$3 > 0) hit[key] = 1; \
	    n = split(file, parts, "/"); pkg = substr(file, 1, length(file) - length(parts[n]) - 1); pkgOf[key] = pkg } \
	  END { \
	    for (k in stmts) { total[pkgOf[k]] += stmts[k]; if (hit[k]) covered[pkgOf[k]] += stmts[k] } \
	    for (p in total) { \
	      rel = substr(p, length(mod) + 2); \
	      if (rel !~ /^(analyzers|internal)(\/|$$)/ || total[p] == 0) continue; \
	      checked++; \
	      pct = 100 * covered[p] / total[p]; \
	      printf "coverage %6.1f%%  %s\n", pct, rel; \
	      if (pct < min) { printf "coverage %.1f%% is below %d%%: %s\n", pct, min, rel > "/dev/stderr"; bad = 1 } \
	    } \
	    if (!checked) { print "coverage: no package of analyzers/ or internal/ was measured" > "/dev/stderr"; bad = 1 } \
	    exit bad }' $(COVER_PROFILE)

vuln:
	go run $(GOVULNCHECK) ./...

# GitHub Actions workflows are valid.
actionlint:
	go run $(ACTIONLINT)

## bench: the benchmark on its pinned corpus, as CI runs it (see internal/cmd/bench)
bench:
	go run ./internal/cmd/bench $(if $(BENCH_OUT),-out $(BENCH_OUT))

## bench-short: only the small generated corpus; no network, about a minute
bench-short:
	go run ./internal/cmd/bench -short $(if $(BENCH_OUT),-out $(BENCH_OUT))

## fuzz: run each fuzz target for FUZZTIME. Their seed inputs already run with make test; a
## failing input is saved under the package's testdata/fuzz and then runs there too.
fuzz:
	go test ./internal/directive -run '^$$' -fuzz '^FuzzDirective$$' -fuzztime $(FUZZTIME)
	go test ./internal/frontend -run '^$$' -fuzz '^FuzzDecode$$' -fuzztime $(FUZZTIME)

## clean: remove what the checks leave behind (the coverage profile, benchmark results)
clean:
	rm -f $(COVER_PROFILE)
	$(if $(BENCH_OUT),rm -rf $(BENCH_OUT))
