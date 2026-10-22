# Constraints

The quality bar for gomemsafe. Every change must meet it before it is handed over for review.

- **Last reviewed:** 2026-10-22, by @Tochemey
- **For agents:** read this file before writing code. Never weaken it to make a change pass.
  Fix the code instead, or ask the owner for an exception.

## Floor (always enforced; a failure blocks the change)

| # | Rule | Reason | Checked by |
|---|------|--------|------------|
| F1 | Every `.go` file starts with the full MIT license text (the text of `LICENSE`, as a `//` comment block). | Owner requirement; every file must carry its licence. | golangci-lint `goheader` |
| F2 | Only allowlisted dependencies (see [Dependency allowlist](#dependency-allowlist)). Adding a module outside it needs a decision in docs/decisions.md first. | Minimal dependencies ([D8](docs/decisions.md)); this is a library, so every dependency reaches its users. | golangci-lint `depguard` + `make check-deps` |
| F3 | No `//nolint` unless it names the linter and gives a reason (`//nolint:gosec // reason`). No `#nosec`. | A silenced check is a hidden exception. | golangci-lint `nolintlint`, `gosec` |
| F4 | No `TODO`, `FIXME`, or unimplemented stubs, and no ignored errors. | Unfinished work must not pass as finished. | golangci-lint `godox`, `errcheck` |
| F5 | No secrets in the repository. Output is always redacted. | A leaked key cannot be un-leaked. | gitleaks `--redact`, with its default rules (`.gitleaks.toml` skips only the gitignored third-party code in `vendor/`) |
| F6 | Tests are never skipped, deleted, or weakened (assertions removed) without the owner's explicit approval. | Tests the agent can switch off prove nothing. | Owner review of every diff |
| F7 | No `bin/` folder. Agents never commit or push. This file is never changed in the same change as the code that fails it. | Owner rules. | Owner review of every diff |
| F8 | Every implementation file `x.go` has a unit test file `x_test.go`, and every `x_test.go` has an `x.go`. | Every piece of code is tested where it lives, and there are no orphaned test files. | `make check-tests` |
| F9 | Code follows the coding standards in [AGENTS.md](AGENTS.md#coding-standards). | One consistent style across the codebase. | golangci-lint `whitespace`, `wsl_v5`, `funcorder`, `revive`; owner review for declaration order, file size, naming, and comments |

## Enforced with numbers

| Dimension | Rule | Reason | Checked by | Runs at |
|-----------|------|--------|------------|---------|
| Format | Zero unformatted files | No style debates in review | `gofmt`, `goimports` (via golangci-lint) | edit |
| Vet | Zero `go vet` findings | The Go team's own baseline | `go vet ./...` | edit |
| Lint | Zero golangci-lint findings | One agreed lint config | `golangci-lint run` | edit |
| Architecture | Engine packages (`internal/directive`, `prefilter`, `lazyssa`, `flow`, `invalidation`, `summary`, `effect`) never import `internal/check/...` or `internal/frontend` | Keeps the engine reusable by every check family (design §4.1) | golangci-lint `depguard` (`engine-layering`) | edit |
| Tests | All tests pass with the race detector on | A memory-safety tool must itself be race-free | `go test -race ./...` | task end |
| Coverage | **≥ 85%** statement coverage per package in `analyzers/` and `internal/` | Analyzer logic is pure and cheap to test; 85% leaves room for error paths that are hard to trigger | `make test` (merged `-coverpkg` profile); the same profile is uploaded to Codecov for the README badge | task end |
| Vulnerabilities | Zero known vulnerabilities reachable from our code, in dependencies or the standard library | Checked against the Go vulnerability database, which is an outside opinion | `govulncheck ./...` | CI |
| Hot-path allocations | 0 allocations per operation in the dataflow engine's steady state | The speed goal (design §7) depends on it | `testing.AllocsPerRun` test in `internal/flow` (added in T4) | task end |
| Go versions | Tests pass on Go 1.26 (the minimum in `go.mod`) and Go 1.27 | Both are supported Go releases | `.github/workflows/ci.yml` matrix, with `GOTOOLCHAIN=local` | CI |
| Workflows | Zero actionlint findings | A broken workflow silently stops enforcing the rest of this file | `actionlint` | CI |
| Supply chain | Every GitHub Action pinned to a full commit SHA; dependencies kept current | A moved tag cannot change what runs in CI | OpenSSF Scorecard (`.github/workflows/scorecard.yml`, Pinned-Dependencies and other checks), Renovate (`.github/renovate.json`) | CI |
| Code scanning | Zero open CodeQL alerts | A second, outside opinion on the code | CodeQL (`.github/workflows/codeql.yml`) | CI |
| Commit messages | Every commit and PR title is a Conventional Commit, with type `build`, `chore`, `ci`, `docs`, `feat`, `fix`, `perf`, `refactor`, `revert`, `style`, or `test` | A readable history, from which release notes can be generated | commitlint with `@commitlint/config-conventional` (`.github/workflows/commitlint.yml`), PR title check (`.github/workflows/pr-title.yml`, rule in `.github/pr-title-checker-config.json`) | CI |

**Where checks run:**
- **edit:** in the edit loop, seconds (`make check-fast`).
- **task end:** before a task is handed over, at most 90 seconds (`make check-task`).
- **CI:** on every pull request (`make check-full`), except one that touches only Markdown.
  CONSTRAINTS.md counts as code, because `make deps` reads its dependency allowlist.

## Measured, not yet enforced

| Metric | Today | Direction | When it becomes enforced |
|--------|-------|-----------|--------------------------|
| Performance budgets (design §7.1: warm re-run, edit-one-package, cold run, peak RSS), measured by the CI `bench` job on zap, GoAkt, and the generated corpus | Not recorded yet: the first CI run produces the baseline | Warn on regression | Set from the first CI run (T11), then must not get worse |

## Dependency allowlist

The modules `go list -m all` may print. Changing this list needs a decision in docs/decisions.md and the
owner's approval.

```
github.com/tochemey/gomemsafe
golang.org/x/tools
github.com/google/go-cmp
github.com/yuin/goldmark
golang.org/x/mod
golang.org/x/net
golang.org/x/sync
golang.org/x/sys
golang.org/x/telemetry
```

Only `golang.org/x/tools` may be imported (enforced by `depguard`). The other modules are
listed because `golang.org/x/tools` requires them, so they appear in the module graph; the
build compiles only the parts of them that x/tools actually uses.

## Tool versions

Pinned to the latest release on 2026-10-22 in the `Makefile`; Renovate proposes each new
release. A version bump is its own change. Every tool runs with `go run pkg@version`, so the pinned version is exactly what runs
and nothing is installed.

| Tool | Version | How it runs |
|------|---------|-------------|
| golangci-lint | v2.15.0 | `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0` |
| govulncheck | v1.9.0 | `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0` |
| gitleaks | v8.30.1 | `go run github.com/zricethezav/gitleaks/v8@v8.30.1` |
| actionlint | v1.7.12 | `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12` |
| GitHub Actions | checkout v7.0.1, setup-go v7.0.0, upload-artifact v7.0.2, download-artifact v8.0.2, codeql-action v4.39.1, scorecard-action v2.5.0, codecov-action v7.1.1, commitlint-github-action v6.2.1, pr-title-checker v2.0.0, paths-filter v4.0.3 | Pinned by commit SHA in `.github/workflows/`; Renovate proposes updates |

## Not used

- **Coverage of changed lines:** no Go-native tool measures it. On this new codebase, the
  per-package 85% rule catches uncovered new code.
- **osv-scanner:** for a Go-only project, govulncheck checks the same vulnerability data more
  precisely, because it only reports vulnerable code that is actually called.

## Exceptions

None. An exception needs an ID (`W1`, `W2`, …), the rule, the path, a reason, an owner, and an
expiry date no more than 90 days away.

| ID | Rule | Path | Reason | Owner | Expires |
|----|------|------|--------|-------|---------|
