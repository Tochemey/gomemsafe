# Architecture decisions

What gomemsafe is built on. Each decision states what we build and why. The design itself is
in [design.md](design.md), and the quality bar is in [CONSTRAINTS.md](../CONSTRAINTS.md).
Changing a decision means editing this file in its own change, with the owner's approval.

## D1. Analysis framework and driver

- Every check is a `golang.org/x/tools/go/analysis` analyzer.
- `cmd/gomemsafe` is a dual-mode binary:
  - When invoked by `go vet` (`-V=full`, `-flags`, or a `vet.cfg` argument), it runs
    `unitchecker.Main`.
  - Otherwise it is the front-end. It runs `go vet -vettool=<self> -json`, renders
    Rust-style diagnostics (or SARIF), and sets the exit code: 0 clean, 1 findings, 2 error.
    JSON `"error"` entries count as errors.
- Users install it as a Go tool (`go get -tool github.com/tochemey/gomemsafe/cmd/gomemsafe`,
  then `go tool gomemsafe ./...`). The analyzers are importable from `analyzers`
  (`All()`, `Default()`), so golangci-lint, gopls, and multichecker run them directly.
- A build-gating `-toolexec` mode is out of scope for v1.

**Why:** `go/analysis` is the industry standard for Go analyzers. The go command already
runs vet tools once per package, in parallel, and caches each run. Its cache key covers the
tool ID, the flags, the package's inputs, and every dependency's facts. With `-json` the tool
exits 0 even when there are findings, so those runs are cached too.

## D2. Modular analysis with function summaries

- One analyzer, `summary`, computes a per-function effect summary and exports it as a fact.
  It is the only analyzer that exports facts. The summary holds:
  - per-parameter bits: `Retains`, `Mutates`, `Consumes`, `Invalidates`, `ShareGoroutine`
  - a `BorrowOf(p)` bit on each result
  - lock effects
- Every check is intra-procedural dataflow on SSA plus callee summaries. There is no
  whole-program pointer analysis and no whole-program call graph.
- Callees that can't be resolved (interface methods, function values, reflection) get a
  conservative default summary, unless an annotation supplies a contract.
- `Consumes` is never inferred. It comes only from annotations or known library effects (`internal/effect`).
- **Speed rules:**
  - Dataflow uses bitset lattices with a per-function instruction × iteration budget. When
    the budget is exceeded, the result is *unknown*, which produces no diagnostic, never a
    slow run.
  - Facts are deterministic: sorted, with no maps, and no fact at all for an empty summary.
    A body-only edit must not change a package's `.vetx` bytes.
  - SSA is built lazily, at most once per package, by an internal `lazyssa` analyzer. It is
    built only for packages where the `prefilter` found a trigger, or where `summary` needs
    it.
  - Checks return immediately in `VetxOnly` runs (dependencies), so dependencies pay only for
    `summary`.
  - Standard-library summaries are computed, not shipped. Hand-written known effects (`internal/effect`) cover only
    behaviour the code can't show (`sync.Pool.Put` invalidates its argument;
    `bufio.Scanner.Scan` invalidates the previous `Bytes()`).

**Why:** this is how Rust's borrow checker works: it is intra-procedural and trusts
signatures. Summaries are Go's stand-in for lifetime signatures. Whole-program analysis
cannot be cached per package and does not scale.

## D3. Checking tiers

| Tier | Applies to | Promise |
|------|-----------|---------|
| 1, Hazards | All code, no annotations needed | High confidence: findings are almost always real bugs. Can miss bugs. |
| 2, Ownership | Code annotated with `owned`, `consumes`, `borrows`, … | Sound within the annotated scope (use after move, borrow escape) |
| 3, Strict | Packages that contain `//memsafe:strict` | No `unsafe`, no cgo, shared mutable state must be guarded |

- A tier describes how strong a promise is. It is not a setting, and there is no `-tier`
  flag. Every analyzer is on by default, and annotations decide where Tier 2 and Tier 3
  apply.
- Analyzers are selected with standard vet flags: `-lifetime` runs only that analyzer, and
  `-cgomem=false` disables one.
- Tier 1 has a zero false-positive budget on the benchmark corpus. A check that can't meet it
  moves to Tier 2 or behind a flag.
- Suppressions take the form `//memsafe:allow MS301 <reason>`, and the reason is required.

**Why:** a sound borrow checker would reject almost all unannotated Go. Tiers let gomemsafe
work on any codebase today, and give Rust-style guarantees where users opt in.

## D4. Annotations

- Annotations are line comments `//memsafe:<verb> [args]`, placed at the end of a
  declaration's doc comment. They add no runtime import and change no API.
- Verbs are single lowercase words: `strict`, `forbid`, `owned`, `consumes`, `borrows`,
  `viewof`, `invalidates`, `allow`.
- A malformed directive (unknown verb, unknown parameter, wrong placement, missing reason) is
  reported as diagnostic `MS000`. It is never silently ignored.
- Annotations on exported declarations reach callers in other packages through `summary`
  facts.

**Why:** comments match Go's own `//go:` directives and staticcheck's `//lint:ignore`, and
adopting them is not a breaking change.

## D5. Concurrency safety

- **MS401/MS402:** variables and maps captured by a goroutine are checked for a write on one
  side and an access on the other, with no recognised happens-before edge between them. The
  recognised edges are `WaitGroup.Wait`, `errgroup.Wait`, a channel receive, and a held
  `Mutex`/`RWMutex`.
- **MS403:** fields annotated as lock-guarded are accessed only while the lock is held (see
  D6).
- **MS501:** sending an `owned` value on a channel is a move, so any later use by the sender
  is an error.
- Everything else is left to `go test -race`.

**Why:** these patterns are cheap to check per function, and they cover the common real
races. General static race detection needs whole-program analysis and produces false
positives.

## D6. Lock annotations use gVisor checklocks syntax

- Lock and atomic contracts use checklocks' annotations verbatim: `// +checklocks:mu`,
  `+checklocksread`, `+checklocksacquire`, `+checklocksrelease`, `+checklocksexclude`,
  `+checklocksignore`, `+checklocksforce`. `+checkatomic`, `+checklocksreadany`, and
  `+checklocksalias` follow later.
- gomemsafe implements the checking itself, on the shared summary and dataflow engine.
- An annotation we recognise but don't support yet is reported, never ignored.
- In CI, our results must agree 100% with checklocks' own test cases for every supported
  annotation.

**Why:** code already annotated for checklocks works unchanged. Importing checklocks itself
would pull in the large gVisor module and a second, separate lock model.

## D7. No configuration file

- Configuration lives in flags (analyzer selection and analyzer options such as
  `-unsafeflow.safety`) and in source directives (`//memsafe:strict`, annotations,
  suppressions).
- Paths are selected with package patterns and `go list`.

**Why:** the go command's vet cache key includes flags and source, but not other files. A
config file would make cached results go stale when it changes.

## D8. Dependencies and tooling

- **Library dependencies:** the standard library and `golang.org/x/tools` (plus the modules
  it requires). Any other module needs a decision recorded here first, approved by the
  owner. It is enforced by golangci-lint `depguard` and `make check-deps` (see
  CONSTRAINTS.md).
- **Dev tools never enter `go.mod`, and nothing is installed into the repository** (no
  `bin/`, no tools module):
  - golangci-lint, govulncheck, and gitleaks run as `go run pkg@version`, pinned in the
    `Makefile`. Each tool builds from its own `go.mod`, so the tools never conflict with each
    other or with gomemsafe.
- All modules and tools use their latest release when pinned. A bump is its own change.

**Why:** gomemsafe is a library, so every module in its `go.mod` takes part in its users'
version selection. x/tools is the one dependency that saves us from rewriting the standard
analysis framework.

## D9. Go version and licensing

- `go.mod` declares `go 1.26.0` (the minimum for users) and `toolchain go1.27.3` (the version
  we build and test with). CI tests on both.
- Every `.go` file starts with the full MIT license text from `LICENSE`.

**Why:** Go supports its two most recent releases, and x/tools requires 1.26. The licence
header is an owner requirement.
