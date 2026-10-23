# Changelog

## Unreleased

- MS301 reports a value used after it was returned to a `sync.Pool`.
- MS301 follows invalidations through helper functions in other packages, using per-function summaries.
- MS302 reports a view from `bytes.Buffer`, `bufio.Scanner`, `bufio.Reader`, or `bufio.Writer` used after a call on its buffer ended it.
- MS101 reports a `uintptr` turned back into a pointer in a way `go vet` cannot see.
- MS103 reports a `reflect.SliceHeader` or `reflect.StringHeader` value used as memory.
- MS104 reports an `unsafe` operation without a `// SAFETY:` comment, enabled with `-unsafeflow.safety`.
- MS000 reports a `//memsafe:` directive that is malformed or not supported.
- Findings name and point at the value used, including in assignments, composite literals, conversions, returns, channel sends, `select` cases, `range` loops, and `go` and `defer` calls.
- `//memsafe:allow <checks> <reason>` suppresses findings on its line and the next, with a mandatory reason.
- `go tool gomemsafe` prints each finding with its source line, related positions, and a help text.
- `-json` prints go vet's JSON output unchanged.
- `-stats` prints the time per analyzer, the share of SSA construction, and peak memory.
- `go vet -vettool=$(go tool -n gomemsafe)` runs every check as a plain vet tool.
- Package `analyzers` exposes every check as a standard `go/analysis` analyzer.
