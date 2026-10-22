# gomemsafe

[![build](https://img.shields.io/github/actions/workflow/status/Tochemey/gomemsafe/ci.yml?branch=main&label=build)](https://github.com/Tochemey/gomemsafe/actions/workflows/ci.yml)
[![codecov](https://img.shields.io/codecov/c/github/Tochemey/gomemsafe/main?label=coverage)](https://codecov.io/gh/Tochemey/gomemsafe)
[![license](https://img.shields.io/github/license/Tochemey/gomemsafe)](LICENSE)

A build-time static analyser that brings Rust-style memory-safety guarantees to Go. It
catches data-race patterns, misuse of `unsafe` and cgo, use of `sync.Pool` values and
borrowed buffer views after they become invalid, and (for code you annotate)
use-after-move and escaping borrows. It has zero runtime cost and needs no language change.

> **Status: M0, the first milestone.** One check is implemented end to end: **MS301**, a value
> used after it was returned to a `sync.Pool`, including through helper functions in other
> packages. The rest of the catalogue in [docs/design.md](docs/design.md) §5 is planned; its
> check IDs are reserved, and its directives are recognised and reported as not yet
> supported.

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

In M0, one directive is enforced:

```go
//memsafe:allow MS301 the buffer is copied before Put returns
buf.WriteString(p)
```

It suppresses the named checks on its own line and on the next line, and the reason is
mandatory. The other directives are recognised, but reported as `MS000: not supported yet`, so
a contract that nothing checks never looks checked.

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
`cgocall` passes are on by default. gomemsafe's lock checks understand
[gVisor checklocks](https://pkg.go.dev/gvisor.dev/gvisor/tools/checklocks) annotations as
written. What it adds, and what no other tool provides, is lifetime checking of
borrowed views (`sync.Pool`, `Buffer.Bytes()`, `Scanner.Bytes()`, `unsafe.String`),
use-after-move, and borrow escape. All of it runs as one cached vet pass. See
[design §1.1](docs/design.md#11-prior-art).

## What a clean run does and does not mean

A clean Tier 1 run means none of the known hazard patterns were found. It is **not** a proof
of memory safety. Only code annotated for Tier 2 or Tier 3 gets a Rust-style guarantee. Keep
running `go test -race`. See [design §9](docs/design.md#9-known-limitations).
