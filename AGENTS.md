# Instructions for agents

These rules apply to every contributor working on gomemsafe, human or agent.

## Before writing code

- Read [CONSTRAINTS.md](CONSTRAINTS.md). It is the quality bar. **Never weaken it to make a
  change pass.** Fix the code instead, or ask the owner for an exception.
- The design is in [docs/design.md](docs/design.md), the architecture decisions are in
  [docs/decisions.md](docs/decisions.md), and how the packages fit together is in
  [docs/maintainers.md](docs/maintainers.md). Documentation states the current design only, with
  no history.

## While working

- Run `make check-fast` while editing, and `make check-task` before handing work over. Both
  must pass.
- Every `.go` file starts with the full MIT license text from `LICENSE`, as a `//` comment
  block.
- Dependencies: the standard library and `golang.org/x/tools` only. Any other module needs an
  owner-approved decision first (decision D8). Dev tools run as `go run pkg@version` and never
  enter `go.mod`.
- Never create a `bin/` folder or install tools into the repository.

## Tests

- Every implementation file `x.go` has a unit test file `x_test.go` next to it, testing what
  `x.go` does. A file with build constraints has a test file with the same constraints.
- There are no other test files: every `x_test.go` belongs to an `x.go`. Shared test helpers
  live in the test file of the code they help test.
- Analyzer test fixtures live under `testdata/` and use the `analysistest` layout
  (`testdata/src/<package>`). The go command ignores `testdata/`, so fixtures are not built.
- `make check-tests` enforces the pairing.

## Coding standards

### File layout

- Put every package-level declaration at the top of the file, after the imports: constants
  first, then variables, then types. Do not declare a `const`, `var`, or `type` between
  functions or methods.
- The exception is a set of constants of a named type (an enum): put its `const` block
  directly after the type declaration.

  ```go
  // Outcome reports whether a dataflow solution is usable.
  type Outcome int

  const (
      Solved Outcome = iota
      Unknown
  )
  ```

- Put exported functions and methods before unexported ones. Do not place an unexported helper
  between exported ones; put it in the file's unexported section, next to the unexported code
  it works with.
- One file per concern. When a file passes about five hundred lines, split it by what it does,
  as `go/parser` splits expressions from statements.

### Blank lines around multi-line blocks

Put a blank line before and after any multi-line statement block: a `{ }`-delimited body
spanning more than one line (`for`, `if`, `switch`, `select`, function literals, composite
literals used as statements).

Exceptions:

- No blank line when the block is the first or last statement in its enclosing block. A blank
  line directly after an opening brace or directly before a closing brace is wrong.
- A simple statement immediately before a block may be grouped with it when it directly sets
  up the block's condition or subject.

Single-line statements forming a related unit may stay grouped without blank lines. The blank
line is only required at the boundary between a multi-line block and the code around it.

```go
fn := ssa.Func(obj)
if fn == nil {
    return 0
}

a := invalidation.Analyze(fn, lookup, eng)
for i, invalid := range a.InvalidAtReturn() {
    if invalid {
        bits |= 1 << uint(i)
    }
}

return bits
```

### Naming

- Names say what a thing is, not where it came from: `siteClasses`, not `sc2`. If a name needs
  its comment to be understood, rename it.
- Unexported helpers are verbs: `closeOverAliases`, `parseArgs`.
- Single-letter receivers, consistent within a type: `a *Analysis`, `e *Engine`.
- Abbreviations only when universal in Go: `pos`, `fn`, `pkg`, `err`, `ssa`.

### Comments

- A comment says why, or what a reader cannot see from the code. It never restates the code.
- Every exported identifier has a doc comment that starts with its name.
- A comment that explains a subtle choice points at the precedent: "mirrors buildssa",
  "see docs/design.md §7.2".
- No banner or separator comments (`// ---- Types ----`, `// =====`). Sections of a file are
  separated by the order of declarations and by blank lines, not by decorated comments.

## Handing work over

- **Do not commit or push.** Leave the changes in the working tree for the owner to review.
  The owner commits.
- Commit messages and PR titles follow [Conventional Commits](https://www.conventionalcommits.org):
  `type(scope): description`, with type one of `build`, `chore`, `ci`, `docs`, `feat`, `fix`,
  `perf`, `refactor`, `revert`, `style`, `test`, and `!` before the colon for a breaking change. CI rejects
  anything else (CONSTRAINTS.md, Commit messages).

## No AI authorship

Do not credit an AI tool, model, or bot as an author anywhere in a contribution. This includes
`Co-Authored-By` commit trailers (e.g. `Co-Authored-By: Claude ...`), "Generated with ..."
footers in PR descriptions, and any other attribution to an AI tool in commits, PR
descriptions, issues, code, or comments. If your tooling adds such lines by default, remove
them before committing and before opening the PR. The human who submits the PR is its sole
author and must be able to explain and defend every line of it in review. PRs that credit an
AI tool as an author will not be reviewed until the attribution is removed.
