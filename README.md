# gomemsafe

[![build](https://img.shields.io/github/actions/workflow/status/Tochemey/gomemsafe/ci.yml?branch=main&label=build)](https://github.com/Tochemey/gomemsafe/actions/workflows/ci.yml)
[![codecov](https://img.shields.io/codecov/c/github/Tochemey/gomemsafe/main?label=coverage)](https://codecov.io/gh/Tochemey/gomemsafe)
[![license](https://img.shields.io/github/license/Tochemey/gomemsafe)](LICENSE)

A build-time static analyser that brings Rust-style memory-safety checks to Go. It finds memory
used after its owner has let it go, such as a value returned to a `sync.Pool` or a slice of a
buffer that has since been reset, and `unsafe` code that hides pointers from the garbage
collector. It runs as a `go vet` tool, with zero runtime cost and no language change.

## Checks

| ID | Reports |
|---|---|
| **MS301** | A value used after it was returned to a `sync.Pool`, including through helper functions in other packages |
| **MS302** | A view, such as the slice from `bytes.Buffer.Bytes` or `bufio.Scanner.Bytes`, or one your own types declare, used after a call on its buffer ended it, including through helper functions in other packages |
| **MS101** | A `uintptr` turned back into a pointer in a way `go vet` cannot see |
| **MS103** | A `reflect.SliceHeader` or `reflect.StringHeader` value used as memory |
| **MS104** | An `unsafe` operation without a `// SAFETY:` comment (opt-in: `-unsafeflow.safety`) |

Every check, with its exact rules and its limits, is defined in the
[check catalogue](docs/design.md#5-check-catalogue). Changes are listed in
[CHANGELOG.md](CHANGELOG.md).

## Quick start

```sh
go get -tool github.com/tochemey/gomemsafe/cmd/gomemsafe
go tool gomemsafe ./...
```

```
server.go:42:3: MS301: buf used after it was returned to sync.Pool
    buf.WriteString(p)
    ^^^
  server.go:40:2: buf returned to pool here
    pool.Put(buf)
  help: move pool.Put(buf) after the last use, or use `defer pool.Put(buf)`
```

## Commands

| Command | Description |
|---|---|
| `go tool gomemsafe ./...` | Run every check, with Rust-style output. Exits 0 when clean, 1 on findings, 2 on errors. |
| `go tool gomemsafe -unsafeflow.safety ./...` | Also report `unsafe` operations without a `// SAFETY:` comment (MS104) |
| `go tool gomemsafe -lifetime=false ./...` | Disable a check (standard vet flag behaviour; `-lifetime` alone runs only that check) |
| `go tool gomemsafe -json ./...` | Print go vet's JSON output unchanged |
| `go tool gomemsafe -stats ./...` | Re-run every package and print the time per analyzer, the SSA share, and peak RSS |
| `go vet -vettool=$(go tool -n gomemsafe) ./...` | Run as a plain vet tool |

## Annotations

You tell gomemsafe more about your code with `//memsafe:` directives, written like `//go:`
directives, plus gVisor checklocks' `// +checklocks:` annotations for lock contracts. The full
annotation language, with every directive, its scope, and examples, is in
[docs/design.md §6](docs/design.md#6-annotation-language). The checks each directive enables
are in the [check catalogue, §5](docs/design.md#5-check-catalogue).

Three directives are enforced today. The first is a suppression:

```go
//memsafe:allow MS301 the buffer is copied before Put returns
buf.WriteString(p)
```

It suppresses the named checks on its own line and on the next line, and the reason is
mandatory. The other two declare the views of your own types, for MS302:

```go
// Bytes returns a view that is valid until the next Reset.
//
//memsafe:viewof b
func (b *Buffer) Bytes() []byte { return b.b }

// Reset empties the buffer.
//
//memsafe:invalidates b
func (b *Buffer) Reset() { b.b = b.b[:0] }
```

`viewof` says that the first result is a view into the named parameter, and `invalidates` that
a call ends every view of it. Callers in any package then get `MS302` for `v := buf.Bytes();
buf.Reset(); use(v)`. The other directives are recognised, but reported as
`MS000: not supported yet`, so a contract that nothing checks never looks checked.

With `-unsafeflow.safety`, every `unsafe` operation needs a `// SAFETY:` comment on the line
above its statement, saying why it is safe:

```go
// SAFETY: b is never written after this point.
s := unsafe.String(&b[0], len(b))
```

## Developing

| Command | Description |
|---|---|
| `make check-fast` | Vet, lint, dependency allowlist, test pairing, secrets (seconds) |
| `make check-task` | `check-fast` plus race-enabled tests and the 85% coverage floor |
| `make check-full` | `check-task` plus govulncheck and actionlint (what CI runs) |
| `make fuzz` | Fuzz the directive parser and the vet JSON decoder (`FUZZTIME`, default 30s each) |
| `make bench-short` | The benchmark on the small generated corpus; CI runs the full corpus (`make bench`) |

To contribute, start with [CONTRIBUTING.md](CONTRIBUTING.md). The quality bar is
[CONSTRAINTS.md](CONSTRAINTS.md), and the coding standards for contributors and agents are in
[AGENTS.md](AGENTS.md).

## Architecture

The checks are standard `go/analysis` analyzers, driven by the go command's vet protocol, so
they also run in golangci-lint and gopls. The go command handles parallelism and
per-package caching. Dependencies are kept to the minimum: the standard library plus
`golang.org/x/tools` (D8). Cross-package knowledge travels only through compact,
deterministic per-function summaries. There is no whole-program analysis,
so the cost after an edit is roughly proportional to the code that changed.

- [docs/decisions.md](docs/decisions.md): the nine architecture decisions (D1–D9)
- [docs/maintainers.md](docs/maintainers.md): how the packages fit together and how to extend them

## How it relates to existing tools

gomemsafe complements plain `go vet`, so keep running it: its `unsafeptr`, `copylock`, and
`cgocall` passes are on by default, and gomemsafe reports only what they cannot see. What it
adds, and what no other tool provides, is lifetime checking: a value returned to a
`sync.Pool`, or a view of a `bytes.Buffer`, `bufio.Scanner`, `bufio.Reader`, or
`bufio.Writer`, used after it stopped being valid. Values returned to a pool, and views and the
calls that end them, are followed through helper functions in other packages. All of it runs as one cached vet pass. See [design §1.1](docs/design.md#11-prior-art).

## What a clean run does and does not mean

A clean run means none of the patterns the checks report were found. It is **not** a proof of
memory safety: each check states, in the catalogue, what it does not follow. Keep running
`go test -race`. See [design §9](docs/design.md#9-known-limitations).
