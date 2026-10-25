# Maintainer guide

How gomemsafe is put together, and how to change it. The design is in
[design.md](design.md), the decisions in [decisions.md](decisions.md), and the quality bar
in [CONSTRAINTS.md](../CONSTRAINTS.md). Every package also has a doc comment
(`go doc ./internal/<package>`) that says what it guarantees.

## How one run works

```
go tool gomemsafe ./...                                      cmd/gomemsafe → frontend
  └─ go vet -vettool=<gomemsafe> -json ./...
       └─ the go command runs gomemsafe once per package, in dependency order, and caches
          every run that exits 0
            └─ cmd/gomemsafe in vet-tool mode
                 ├─ vetmode: is this a facts-only run (a dependency)?
                 └─ unitchecker.Main(analyzers.All()...)
                      ├─ directive    parse //memsafe: comments, view contracts; report MS000
                      ├─ prefilter    which function calls which static callee, and passes it
                      │               a parameter
                      ├─ lazyssa      SSA for the package, built only if asked
                      ├─ summary      per-function facts → .vetx (all packages): the arguments
                      │               taken back, the view returned, the views ended
                      └─ checks       lifetime (MS301, MS302), skipped in facts-only runs;
                                      unsafeflow (MS101, MS103; MS104 with -unsafeflow.safety), not run in facts-only runs
                           └─ lifetime: invalidation + flow, must-dataflow on SSA
                           └─ locate: points diagnostics at the value used
                           └─ report: emits diagnostics, honouring //memsafe:allow
  └─ frontend decodes the JSON, renders it, exits 0 / 1 / 2
```

## Package map

| Package | Role | Depends on |
|---|---|---|
| `analyzers` | Public API: `All()`, `Default()` | checks, directive |
| `cmd/gomemsafe` | The binary; picks front-end or vet-tool mode | analyzers, frontend, trace, vetmode |
| `internal/catalog` | The stable check IDs (`MS301`, …), implemented and planned | none |
| `internal/check/lifetime` | MS301, use after invalidation; MS302, a view used after its owner ended it | engine packages, locate, report |
| `internal/check/unsafeflow` | MS101, a uintptr turned back into a pointer where vet cannot see it; MS103, a reflect header value used as memory; MS104 (opt-in), an unsafe operation without a `// SAFETY:` comment | catalog, directive, report |
| `internal/cmd/bench` | Development command: the benchmark harness (design §7.1) | frontend, trace |
| `internal/cmd/rsswrap` | Development command: `-toolexec` wrapper recording each compile's peak RSS | trace |
| `internal/directive` | `//memsafe:` parsing: suppressions, view contracts (`viewof`, `invalidates`); MS000 | catalog, effect, vetmode |
| `internal/flow` | Must-dataflow engine over SSA | none |
| `internal/frontend` | Runs vet, decodes its JSON (`Decode`), renders, exit codes, `-stats` | trace |
| `internal/effect` | Known effects of library calls: invalidations (`sync.Pool.Put`), and the standard library's views and the events that end them | none |
| `internal/invalidation` | Tracks invalidated values, and views that were ended, through one function; the view a function returns and the events it is on its parameters, for summaries | effect, flow |
| `internal/lazyssa` | Builds SSA on demand, once | trace |
| `internal/locate` | Maps SSA instructions back to the source expressions of the values they use | none |
| `internal/prefilter` | Static callees per function, and those passed a parameter; calls by position | none |
| `internal/report` | The one way checks emit diagnostics | directive |
| `internal/summary` | Facts of what each function does to its arguments (taken back, viewed, views ended), the only fact producer | directive, effect, flow, invalidation, lazyssa, prefilter |
| `internal/trace` | `GOMEMSAFE_TRACE` timing events | vetmode |
| `internal/vetmode` | Vet-tool invocation and facts-only detection | none |

The engine packages (`directive`, `prefilter`, `lazyssa`, `flow`, `invalidation`, `summary`,
`effect`) never import a check or the front-end. `depguard` enforces this
([CONSTRAINTS.md](../CONSTRAINTS.md), Architecture).

## Common changes

### Teach gomemsafe about another invalidating function

There are two kinds of effect, both in `internal/effect/effect.go`, keyed by the callee's full
name as `types.Func.FullName` prints it, declared as a constant.

- **An invalidation** takes an argument back, like `sync.Pool.Put`: add an `Invalidation` entry
  to `known`. Argument indexes count the receiver as 0. Functions that only *call* such a
  function need nothing: `summary` derives their facts.
- **A view** is a result that points into the receiver's buffer, like `bufio.Scanner.Bytes`:
  add it to `views` with the events that end it, and add the calls that are those events, like
  `bufio.Scanner.Scan`, to `events`. Quote the documentation sentence each entry relies on.
  When the documentation does not say a call ends a view, leave the call out. Functions that
  return such a view of a parameter, or call such an event on one, need nothing: `summary`
  derives their facts too. Code outside the standard library declares its own views with
  `//memsafe:viewof` and `//memsafe:invalidates` instead.

Then:

1. Test the entry in `internal/effect/effect_test.go`, with `effect.Of`, `effect.ViewOf`, or
   `effect.EventsOf`.
2. Add positive cases and look-alike negative cases to the fixtures under
   `internal/check/lifetime/testdata/src`.

### Add a check

1. Add its ID to `internal/catalog` (and to `docs/design.md` §5, if it is new to the
   catalogue).
2. Create `internal/check/<family>/<family>.go` with an `Analyzer` that:
   - requires what it needs (usually `directive`, `prefilter`, `lazyssa`, `summary`),
   - if it requires `summary`, returns immediately when `vetmode.FactsOnly()` is true and
     records `trace.KindSkipped`. unitchecker runs such a check on every dependency only
     because of the fact producer it requires; a check that requires none is not run there,
     and needs no such guard,
   - emits every diagnostic through `report.Report`, with `Category` set to its ID,
   - exports no facts. If it needs cross-package knowledge, extend `summary.Fact` instead
     (decision D2).
3. Append the analyzer to `analyzers.All()`.
4. Write `<family>_test.go` with `analysistest` and fixtures under `testdata/src`, covering
   both findings and a false-positive suite of correct code that must stay silent.

### Implement a pending directive verb

Remove it from `pendingVerbs` in `internal/directive/directive.go`, parse its arguments in
`parse`, expose what checks need through `Result`, and add fixtures to
`internal/directive/testdata`.

### Change the summary fact

`summary.Fact` is an on-disk format: changing it invalidates every cached run, which is
acceptable but should be rare. Keep it free of maps and positions, so that the facts file
stays byte-identical across edits that do not change a summary.
`TestFactsAreByteIdenticalAcrossBodyOnlyEdit` in `internal/frontend` guards this.

## Debugging and measuring

- `go tool gomemsafe -stats ./...` reruns every package (`go vet -a`) and prints the time per
  analyzer, the SSA share, dataflow budget hits, and peak RSS.
- `GOMEMSAFE_TRACE=<dir> go vet -vettool=$(go tool -n gomemsafe) ./...` writes the raw events,
  one JSON line each. Use a fresh `GOCACHE`: cached packages record nothing.
- `go vet -vettool=<binary> -json -work -x ./...` keeps the work directory, where each
  package's `vet.cfg` and its facts file (`VetxOutput`) can be inspected.
- The CI `bench` job measures every budget of design §7.1 on the pinned corpus in
  `internal/cmd/bench/corpus.json` (zap, GoAkt, and a generated corpus) and puts the results table in
  the job summary. Locally, `make bench-short` runs only the generated corpus;
  `go run ./internal/cmd/bench` takes `-corpus`, `-scenario`, and `-out`. A run leaves nothing
  behind, even when interrupted. To move a pin, change its tag and commit together.
- A function that exceeds the dataflow budget reports nothing and counts as a budget hit.
  Raise `flow.DefaultBudget` only with a benchmark that shows the need.

## Conventions

The coding standards and test rules are in [AGENTS.md](../AGENTS.md); in short: one
`x_test.go` per `x.go`, declarations before functions, exported before unexported, and a
license header on every file. `make check-fast` while editing, `make check-task` before
handing work over.
