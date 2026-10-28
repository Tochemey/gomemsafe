# Changelog

## Unreleased

- MS301 reports a value used after it was returned to a `sync.Pool`.
- MS301 follows invalidations through helper functions in other packages, using per-function summaries.
- MS302 reports a view from `bytes.Buffer`, `bufio.Scanner`, `bufio.Reader`, or `bufio.Writer` used after a call on its buffer ended it.
- `//memsafe:viewof` and `//memsafe:invalidates` declare the views of other types and the calls that end them, which MS302 follows in callers in any package.
- MS302 follows views through helper functions in other packages, using per-function summaries: a helper that returns a view of a parameter, or ends a parameter's views.
- MS101 reports a `uintptr` turned back into a pointer in a way `go vet` cannot see.
- MS102 reports a string used after a write into the bytes it shares with a `[]byte` through `unsafe.String` or `unsafe.Slice`.
- MS103 reports a `reflect.SliceHeader` or `reflect.StringHeader` value used as memory.
- MS104 reports an `unsafe` operation without a `// SAFETY:` comment, enabled with `-unsafeflow.safety`.
- MS401 reports a variable a goroutine captures, written on one side and accessed on the other with no synchronisation between them, following synchronisation through helper functions in other packages.
- MS501 reports a value used after its ownership moved: an `//memsafe:owned` value sent on a channel or stored into another owner, or a value passed to a `//memsafe:consumes` parameter, in any package.
- MS000 reports a `//memsafe:` directive that is malformed or not supported.
- Findings name and point at the value used, including in assignments, composite literals, conversions, returns, channel sends, `select` cases, `range` loops, and `go` and `defer` calls.
- `//memsafe:allow <checks> <reason>` suppresses findings on its line and the next, with a mandatory reason.
- `go tool gomemsafe` prints each finding with its source line, related positions, and a help text.
- `-json` prints go vet's JSON output unchanged.
- `-stats` prints the time per analyzer, the share of SSA construction, and peak memory.
- `go vet -vettool=$(go tool -n gomemsafe)` runs every check as a plain vet tool.
- Package `analyzers` exposes every check as a standard `go/analysis` analyzer.
