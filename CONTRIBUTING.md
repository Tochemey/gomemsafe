# Contributing to gomemsafe

Contributions are welcome: bug reports, false positives found in real code, new checks,
fixes, and documentation. This guide covers how to build, check, and submit a change. The
rules it refers to live in three files:

- [CONSTRAINTS.md](CONSTRAINTS.md) is the quality bar: the checks every change must pass.
- [AGENTS.md](AGENTS.md) holds the coding standards and the test rules, for human and AI
  contributors alike.
- [docs/maintainers.md](docs/maintainers.md) explains how the packages fit together and how
  to make the common changes.

## Prerequisites

- Go 1.26 or later. `go.mod` selects the 1.27 toolchain, which the go command downloads if
  needed.
- `make` and `git`.

Nothing else needs installing. Every dev tool (golangci-lint, govulncheck, gitleaks,
actionlint) runs through `go run pkg@version` at the version pinned in the `Makefile`.

## Getting started

1. Fork the repository and clone your fork:

   ```sh
   git clone https://github.com/<your-username>/gomemsafe.git
   cd gomemsafe
   ```

2. Check that everything passes before you change anything:

   ```sh
   make check-task
   ```

## Make targets

| Target | Purpose |
|---|---|
| `make check-fast` | Vet, lint, dependency allowlist, test pairing, and secrets. Seconds: run it while editing. |
| `make check-task` | `check-fast`, plus the tests with the race detector and the 85% coverage floor. Run it before you open a pull request. |
| `make check-full` | `check-task`, plus govulncheck and actionlint. This is what CI runs. |
| `make fuzz` | Fuzz the directive parser and the vet JSON decoder, `FUZZTIME` each (default 30s). |
| `make bench-short` | The benchmark on a small generated corpus, about a minute, with no network. CI runs the full corpus. |
| `make clean` | Remove what the checks leave behind. |

## Making a change

1. **Start from an issue** for anything larger than a small fix, so the approach can be
   agreed before you write it. A new check or directive also needs its place in
   [docs/design.md](docs/design.md).
2. **Follow the rules in [AGENTS.md](AGENTS.md).** In short:
   - Every `.go` file starts with the MIT license header.
   - Every `x.go` has its unit tests in `x_test.go`, and there are no other test files.
   - Declarations come before functions, exported code before unexported.
   - The only dependencies are the standard library and `golang.org/x/tools`. Another module
     needs an approved decision first (decision D8 in [docs/decisions.md](docs/decisions.md)).
3. **Test what you change.**
   - A check change needs analyzer fixtures under `testdata/src`: cases it must report, and
     look-alike correct code it must stay silent on.
   - A bug fix needs a test that fails without the fix.
4. **Never weaken [CONSTRAINTS.md](CONSTRAINTS.md) to make a change pass.** Fix the code, or
   ask for an exception in the pull request.
5. **Run `make check-task`** and make sure it passes.

[docs/maintainers.md](docs/maintainers.md) has step-by-step recipes for the common changes:
teaching gomemsafe about another invalidating function, adding a check, and implementing a
pending directive.

### False positives

A finding on correct code is a bug, and the most valuable report you can send. Open an issue
with the smallest code that reproduces it and the diagnostic gomemsafe printed. If you fix it,
add that code to the check's false-positive fixtures (for MS301,
`internal/check/lifetime/testdata/src/falsepos`) so it stays fixed.

## Commits and pull requests

- **Commit messages and pull request titles follow
  [Conventional Commits](https://www.conventionalcommits.org).** Write them as
  `type(scope): description`, where type is one of `build`, `chore`, `ci`, `docs`, `feat`,
  `fix`, `perf`, `refactor`, `revert`, `style`, or `test`. Put a `!` before the colon for a
  breaking change, for example `feat(lifetime)!: ...`. CI rejects anything else.
- **Keep one change per pull request.** A refactor and a new feature are two pull requests.
- **Open the pull request against `main`.** These checks must pass:
  - `ci`, which sums up `make check-full` on Go 1.26 and Go 1.27 and the benchmark, whose
    results appear in the job summary
  - commitlint and the PR title check
  - CodeQL

  A change that touches only Markdown files skips the build and CodeQL; `ci`, commitlint, and
  the PR title check still report. CONSTRAINTS.md is the exception: a change to it builds.

## Authorship

Whoever submits a change must be able to explain and defend every line of it in review. You
are the author of your pull request.

Do not credit an AI tool as an author: no `Co-Authored-By` trailers naming an AI tool, no
"Generated with ..." lines in the pull request description, and no AI attribution in code,
comments, or issues. If your tooling adds these lines by default, remove them before you
commit. Pull requests that credit an AI tool as an author will not be reviewed until the
attribution is removed.

## Reporting security issues

Do not open a public issue for a vulnerability. Report it privately as described in
[SECURITY.md](SECURITY.md).

## Code of conduct

Everyone taking part in this project follows the [code of conduct](CODE_OF_CONDUCT.md).

## License

By contributing, you agree that your contributions are licensed under the
[MIT License](LICENSE), like the rest of gomemsafe.
