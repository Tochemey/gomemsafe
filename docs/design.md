# gomemsafe — Design

- **Decisions:** [decisions.md](decisions.md)

## 1. Problem

Go is memory-safe *most of the time*. The garbage collector rules out use-after-free and
double-free of Go-managed memory, slices and strings are bounds-checked, and every value is
zero-initialised. Rust gets most of the same guarantees through ownership instead of a GC, so
on those axes the two languages are already even.

Go has five gaps that Rust closes at compile time:

| # | Gap | Why it is a memory-safety issue in Go |
|---|-----|---------------------------------------|
| 1 | **Data races** | The Go memory model states that racing on multi-word values (interfaces, slices, strings) can produce values that no single write ever stored, and this "can lead to arbitrary memory corruption." Concurrent map writes crash the runtime with a fatal error. Rust prevents this with `Send`/`Sync` and the aliasing-XOR-mutation rule. |
| 2 | **`unsafe`** | `unsafe.Pointer` has six documented valid patterns. Anything else is undefined behaviour that the compiler accepts without complaint. |
| 3 | **cgo** | C memory is not GC-managed (use-after-free, double-free, leaks). Breaking the cgo pointer-passing rules is caught only at runtime, and only partly (`GODEBUG=cgocheck`). |
| 4 | **Borrowed views with a lifetime** | `sync.Pool`, `bytes.Buffer.Bytes()`, `bufio.Scanner.Bytes()`, `bufio.Reader.ReadSlice`, `unsafe.String`, mmap'd regions. The GC keeps the bytes alive, but the *contents* get reused, so the result is a logical use-after-free: silent data corruption with no crash. Rust lifetimes reject this. |
| 5 | **Aliased mutation** | A pointer handed to another goroutine or channel and then mutated by the sender, or `append` aliasing a backing array that someone else still holds. Rust's move semantics and exclusive `&mut` reject both. |

**gomemsafe** is a build-time static analyser that brings the checkable subset of Rust's
guarantees to unmodified Go code. It has no runtime cost and needs no language change.

### 1.1 Prior art

No existing tool covers gaps 4 and 5, and none combines all five into one fast suite.
Several tools cover parts of gaps 1–3, and gomemsafe complements them, extends them, or
stays compatible with them rather than competing:

| Tool | What it covers | Relationship to gomemsafe |
|---|---|---|
| `go vet`: `unsafeptr`, `copylock`, `cgocall` | Single-expression `unsafe.Pointer` misuse, copied locks, some breaks of the cgo pointer rules | **Complementary.** Plain `go vet` already runs these by default, so keep running it. MS101 covers the uintptr round trips `unsafeptr` cannot see, and MS201 adds the flow tracking `cgocall` lacks, without duplicating either. |
| [gVisor checklocks](https://pkg.go.dev/gvisor.dev/gvisor/tools/checklocks) | Lock and atomic contracts through `+checklocks:mu` annotations | **Annotation-compatible.** MS403 uses its syntax verbatim and is implemented in our engine ([D6](decisions.md)). |
| [go-safer](https://github.com/jlauinger/go-safer) | `reflect.SliceHeader`/`StringHeader` misuse; unsafe struct casts (last release 2020) | Overlaps MS103, which reports a header value where it is turned into memory. Unsafe struct casts are not covered. |
| [Chronos](https://github.com/amit-davidson/Chronos) | Whole-program static race detection; by its own README it struggles with large programs and ignores channels and WaitGroups (inactive since 2022) | A whole-program approach; its limitations are why [D5](decisions.md) checks narrow patterns instead. |
| [Gobra](https://arxiv.org/pdf/2105.13840) | Formal verification of Go with permission-based specs and an SMT solver; proves the absence of races | Strongest guarantees, but needs a full spec on every function and is not a build-time check. [D3](decisions.md) explains why we don't take that route. |
| `go test -race` | Races that actually happen at runtime | Complementary. |
| [nilaway](https://github.com/uber-go/nilaway) | Nil safety | Out of scope here. |

**What gomemsafe adds that does not exist elsewhere:** lifetime checking of borrowed views
(MS301–MS303: `sync.Pool`, `Buffer.Bytes()`, `Scanner.Bytes()`, `unsafe.String`), ownership
transfer and use-after-move (MS501), borrow escape (MS502), and modular summaries that let all
of it run as one cached vet pass.

## 2. Goals and non-goals

### Goals

- **G1 — Catch gaps 1–5 at build time** and report them in Rust's style: the primary
  location, the related locations ("moved here", "returned to pool here"), and a fix hint.
- **G2 — Be fast enough to run on every build.** See §7 for budgets. A finding nobody waits
  for is worth more than a precise finding nobody runs.
- **G3 — Zero runtime footprint.** Annotations are comments. Users never import a runtime
  package.
- **G4 — Drop-in integration:** `go vet -vettool`, `go tool gomemsafe`, golangci-lint,
  gopls, and an importable `[]*analysis.Analyzer` for custom drivers.
- **G5 — Incremental adoption.** The default checks are useful with no annotations and have a
  very low false-positive rate. Stronger, Rust-like guarantees are opt-in per type or per
  package ([D3](decisions.md)).
- **G6 — Minimal dependencies.** The standard library first. Go-team modules
  (`golang.org/x/*`) only when they replace something we would otherwise write; today that
  is `golang.org/x/tools`. Anything else needs an owner-approved decision. Dev tools never
  enter `go.mod` and nothing is installed into the repository ([D8](decisions.md)).

### Non-goals

- **A full Rust borrow checker for unannotated Go.** Go has no lifetime syntax, and idiomatic
  Go shares pointers freely. A sound checker would reject almost every real program. We aim
  for soundness only *within the scope of what users annotate*. Elsewhere we report
  high-confidence hazards.
- **Replacing `go test -race`.** The race detector finds races that actually happen at
  runtime. gomemsafe finds patterns on code paths that tests never exercise. Use both.
- **Nil safety**, already covered by [nilaway](https://github.com/uber-go/nilaway). Goroutine
  leaks and general resource leaks may be added later. They are not memory safety.
- **Re-implementing existing `go vet` passes.** `unsafeptr`, `copylock`, and `cgocall` run in
  plain `go vet` by default. gomemsafe extends what they find, and does not duplicate them.
- **A new lock-annotation language.** Lock contracts use gVisor checklocks' syntax
  ([D6](decisions.md)).
- **Changing the compiler or the language.**

## 3. What "Rust-like" means here

| Rust guarantee | Go hazard today | gomemsafe check | Tier |
|---|---|---|---|
| Lifetimes on borrows | Use after `sync.Pool.Put`; `Buffer.Bytes()`/`Scanner.Bytes()` views that outlive their validity | MS301–MS303 | 1 |
| Aliasing XOR mutation | Mutating the `[]byte` behind an `unsafe.String`; `append` aliasing | MS102, MS503 | 1 / 2 |
| `Send` / `Sync` | Goroutine-captured variables written without a happens-before edge; concurrent map writes; unguarded fields | MS401–MS403 | 1 (+ annotations) |
| `unsafe` is quarantined and justified | A `uintptr` turned back into a pointer where vet cannot see it; `reflect.SliceHeader`; unjustified `unsafe` | MS101, MS103, MS104 | 1 |
| FFI hygiene | Breaking cgo pointer rules; use of C memory after `C.free`; double free | MS201–MS203 | 1 |
| Ownership / move | A value sent on a channel or handed to a consuming function, then used again | MS501 | 2 |
| Borrows do not escape | A borrowed parameter retained in a field, a global, a goroutine, or a return value | MS502 | 2 |
| `#![forbid(unsafe_code)]` | — | Strict package mode (MS9xx) | 3 |

## 4. Architecture

The package-level view, with how-to recipes, is in [maintainers.md](maintainers.md).

```
  go tool gomemsafe ./...         go vet -vettool=$(go tool -n gomemsafe) ./...   golangci-lint / gopls
          │                                   │                                      │
          └─ front-end mode: exec "go vet ────┤                                      │
             -vettool=self -json", render,    │                                      │
             set exit code                    ▼                                      │
                                   go command (cmd/go)                               │
                        schedules packages in dependency order, in parallel,         │
                        caches every per-package run in GOCACHE                      │
                                              │ one process per package              │
                                              ▼                                      ▼
                              unitchecker (vet protocol) ──────────────  analyzers.All()
                                              │
             ┌────────────────────────────────┼─────────────────────────────────┐
             ▼                                ▼                                 ▼
      directive                          prefilter                          summary
   parse //memsafe:* and          cheap AST + types scan;               per-function effect
   +checklocks once per package   decides which functions               summaries exported
                                  need deep analysis                    as facts (.vetx)
             └────────────────────────────────┬─────────────────────────────────┘
                                              ▼        (short-circuited in VetxOnly runs)
          check analyzers (unsafeflow, cgomem, lifetime, sharing, locks, ownership, strict)
                   intra-procedural dataflow on lazily built SSA + callee summaries
                                              ▼
                     analysis.Diagnostic {Pos, Category: "MS301", Related, SuggestedFixes}
```

### 4.1 Components

| Package | Responsibility |
|---|---|
Dependencies are the standard library plus `golang.org/x/tools`
([D8](decisions.md)).

| Package | Responsibility |
|---|---|
| `analyzers` | Public API: `All()`, `Default()`, and each analyzer individually. |
| `internal/directive` | Parses `//memsafe:` directives and checklocks annotations (`+checklocks…`, `+checkatomic`) once per package. Other analyzers consume the result through `Requires`. Malformed directives are reported as diagnostics, never silently ignored. |
| `internal/prefilter` | A single `inspector` pass that records *trigger sites*: `unsafe` and `"C"` imports, `go` statements, channel sends of pointer-containing types, uses of objects with a known effect (`sync.Pool`, `bytes.Buffer.Bytes`, …), annotated objects, and calls to functions that have non-empty summary facts. Functions with no trigger sites are never lowered to SSA dataflow. |
| `internal/summary` | **The only analyzer that exports facts** ([D2](decisions.md)). Computes a compact effect summary for each function and method (see §4.2). |
| `internal/effect` | The known effects of library calls whose semantics cannot be derived from their code (`sync.Pool.Put` invalidates its argument; `Scanner.Scan` invalidates the previous `Bytes()`; `WaitGroup.Wait` completes a happens-before edge, and `Mutex.Unlock` does not). A known effect takes precedence over a computed summary. |
| `internal/lazyssa` | Builds a package's SSA once, on first request, and shares it between `summary` and the checks. It replaces `buildssa`, which always builds and pulls in `ctrlflow` (§7.2 item 5). |
| `internal/flow` | A shared dataflow engine: sparse tracked values, bitset lattices, a worklist in reverse postorder, and per-function budgets (§7.3). |
| `internal/check/*` | One analyzer per check family. Each is thin: it defines *sources* (what creates a tracked value), *transfer* rules (what invalidates, moves, or escapes it), and *sinks* (what counts as a violating use). |
| `internal/frontend` | Front-end mode: runs `go vet -vettool=self -json`, renders diagnostics, decides the exit code. |
| `internal/invalidation` | The analysis behind MS301, MS302, and the summaries: tracks values a call took back, and views a call on their owner ended, through one SSA function, by alias class. |
| `internal/catalog` | The stable check IDs. |
| `internal/report` | The one way checks emit diagnostics, honouring `//memsafe:allow`. |
| `internal/locate` | Maps an SSA instruction back to the source expression of the value it uses, so that diagnostics point at that value. |
| `internal/vetmode` | Detects vet-tool invocations and facts-only (`VetxOnly`) runs. |
| `internal/trace` | `GOMEMSAFE_TRACE` timing events, used by `-stats` and the benchmarks. |
| `internal/cmd/bench`, `internal/cmd/rsswrap` | Development commands: the benchmark harness for §7.1, and the `-toolexec` wrapper that measures compile memory. |
| `cmd/gomemsafe` | A dual-mode binary ([D1](decisions.md)). |

### 4.2 Function summaries — Go's stand-in for lifetime signatures

Rust's borrow checker is intra-procedural. It never looks inside a callee because the
callee's **signature** (lifetimes, `&` vs `&mut`, by-value moves) tells it everything it
needs. Go signatures carry none of that information, so gomemsafe **infers a signature-like
summary** for each function and exports it as an `analysis.Fact`. Callers in other packages
read the fact, not the callee's body.

Each parameter (including the receiver) gets a small bitset:

| Effect bit | Meaning | Rust analogue |
|---|---|---|
| `Retains` | May be stored somewhere that outlives the call (heap, global, goroutine, returned) | `'static` / lifetime longer than the call |
| `Mutates` | May write through the parameter | `&mut T` |
| `Consumes` | Ownership moves to the callee: the caller must not use it again (annotation or known effect only, never inferred) | by-value `T` |
| `Invalidates` | Invalidates borrows previously derived from this parameter (e.g. `Reset`) | `&mut self` ending outstanding borrows |
| `ShareGoroutine` | May be made reachable from another goroutine | requires `Send` |

Results carry a `BorrowOf(p)` bit: the result is a view into parameter `p` and stays valid
only until `p` is invalidated (`Buffer.Bytes`). The function as a whole carries a `Syncs` bit:
a call of it may synchronise with another goroutine (MS401). Lock effects (`Acquires`,
`Releases`, `Requires(mu)`) feed MS403.

When the callee cannot be resolved (an interface method, a function value, reflection), the
caller uses a **conservative default**: `Retains | Mutates`, without `Consumes` or
`Invalidates`. If an interface method is annotated, that annotation is the contract, and
every implementation in the analysed packages is checked against it, the way a trait
signature binds its impls.

## 5. Check catalogue

Check IDs (`MS301`) are stable. They appear in diagnostics and in `//memsafe:allow`
suppressions. On the command line, checks are selected by **analyzer name** with the standard
vet flags (§8), like any other vet tool.

A **tier** is how strong a check's promise is (D3). It is not a setting. Every analyzer
is on by default. Tier 2 checks fire only where ownership annotations exist, and Tier 3 checks
fire only in packages that contain `//memsafe:strict`. The code itself decides which tier
applies, so the command line never mentions tiers.

| Analyzer | Checks | Tier | Opt-in sub-checks (analyzer flags) |
|---|---|---|---|
| `unsafeflow` | MS101–MS104 | 1 | `-unsafeflow.safety` enables MS104 |
| `cgomem` | MS201–MS203 | 1 | — |
| `lifetime` | MS301–MS303 | 1 | — |
| `sharing` | MS401–MS402 | 1 | — |
| `locks` | MS403 | 1, on annotated code | — |
| `ownership` | MS501–MS503 | 2, on annotated code | `-ownership.appendalias` enables MS503 on unannotated slices |
| `strict` | MS901–MS903 | 3, in `//memsafe:strict` packages | — |

### Tier 1 — Hazards (on by default, no annotations required)

| ID | Check | Example |
|---|---|---|
| MS101 | A `uintptr` turned back into a pointer without a conversion vet can see: its memory read as a pointer type, or `unsafe.Add` on a nil pointer. Vet's `unsafeptr` already reports every `uintptr`-to-`unsafe.Pointer` conversion outside the allowed forms, so MS101 does not repeat those. An address outside the Go heap (machine code, C or `mmap` memory) is reported too; suppress it with a reason. | `*(*unsafe.Pointer)(unsafe.Pointer(&u))` / `unsafe.Add(nil, u)` |
| MS102 | A string used after a write into the bytes it shares with a byte buffer, which breaks string immutability. The string is made with `unsafe.String(&b[i], n)` or `unsafe.String(unsafe.SliceData(b), n)` over a byte slice or array `b`, or the buffer with `unsafe.Slice(unsafe.StringData(s), n)` over the string `s`. A write counts only where it lands in the string's bytes: any write into `b`, a reslice `b[k:]` of it, `copy`, `clear`, or a whole-array assignment when the string covers all of `b` (its length is `len(b)`, and every write into an `unsafe.Slice` view of a string); otherwise a store at a constant index inside a constant-length string, a store at the string's first byte, a `clear(b)`, or a `copy` into `b` when the string starts at `b[0]`. The use must follow such a write on every path. `len`, a comparison with `""`, and `unsafe.StringData` read no bytes. `append`, a write inside a called function, and a buffer loaded from a field or captured by a function literal are not followed. | `s := unsafe.String(&b[0], n); b[0] = 'x'; use(s)` |
| MS103 | A `reflect.SliceHeader` / `reflect.StringHeader` value, not a pointer to a real slice or string, whose address is taken as an `unsafe.Pointer`. Its `Data` field is a `uintptr` the garbage collector does not follow, so the slice or string made from it points at memory nothing keeps alive. A header's `Data` read as a pointer is MS103's, not MS101's: correct through a header pointer, this misuse through a header value. | `h := reflect.SliceHeader{Data: d, Len: n, Cap: n}; b := *(*[]byte)(unsafe.Pointer(&h))` |
| MS104 | *(opt-in: `-unsafeflow.safety`)* An `unsafe` operation without a `// SAFETY:` justification comment (like clippy's `undocumented_unsafe_blocks`). The operations are conversions to or from `unsafe.Pointer` and `unsafe.Add`, `Slice`, `SliceData`, `String`, and `StringData`; `Sizeof`, `Alignof`, and `Offsetof` only compute constants. The comment ends on the line directly above the statement, has a line starting with `SAFETY:` followed by the reason, and covers every operation in that statement. Each line is reported once. Generated files, such as cgo's, are skipped: their justification belongs in the generator. | `t := unsafe.String(&c[0], len(c))` with no comment above it |
| MS201 | A Go pointer passed to C where the pointed-to memory itself contains Go pointers (a static version of `cgocheck`) | — |
| MS202 | C memory used after `C.free`, or freed twice, within one function | — |
| MS203 | `C.CString`/`C.CBytes`/`C.malloc` result not freed on every path and not returned *(warning)* | — |
| MS301 | Use of a value after `sync.Pool.Put` | `pool.Put(buf); buf.WriteString("x")` |
| MS302 | Use of a borrowed view after its owner is invalidated. The views and the calls that end them come from the standard library's documentation: `bytes.Buffer` (`Bytes`, `Next`, `Peek`, `AvailableBuffer`), `bufio.Scanner.Bytes`, `bufio.Reader` (`Peek`, `ReadSlice`, `ReadLine`), and `bufio.Writer.AvailableBuffer`. A call the documentation does not name does not end a view: `bufio.Reader.Peek` after `Peek`, `bytes.Buffer.Grow`, and `Reset` or `Truncate` after any view but `Bytes`. Other types declare their views with `//memsafe:viewof` and the calls that end them with `//memsafe:invalidates` (§6): a declared view is ended by any call that ends its owner's views, and a declared invalidation ends every view of its argument. A helper that returns a view of one of its parameters on every path, or ends a parameter's views on every path, is followed in its callers in any package through its summary. A helper's view of a field of a parameter, or its call on such a field, is not, because ending the views of one field leaves those of the others valid. The owner must be the same buffer at both calls: a local variable, a parameter, a global, or a field such a value holds by value, such as `c.buf` or an embedded buffer. A buffer behind a pointer field, or captured by a function literal, is loaded at each call and could be reassigned between them, so it is not followed; neither is an owner the function replaces after a view of it was made, such as `buf = bytes.Buffer{}` or `*c = conn{}`, since the view keeps the old buffer. A view carried round one loop is followed when its owner is defined before the loop; one kept across nested loops is missed. `len` and `cap` are no use. | `b := buf.Bytes(); buf.Reset(); use(b)` / `x := sc.Bytes(); sc.Scan(); use(x)` |
| MS303 | A borrowed view escapes (to a field, a global, a channel, or a goroutine) while its owner is later reused | `results = append(results, sc.Bytes())` inside a scan loop |
| MS401 | A variable that a goroutine's function literal captures, written on one side and read or written on the other, with nothing between the two accesses that may order them. The sides are the goroutine and the function that starts it, after the `go` statement. A pair is reported when some path from the `go` statement reaches the starting function's access without synchronising, and some path from the goroutine's start reaches its access without synchronising. Synchronising is a channel send or receive, a `select`, a call the Go memory model documents as completing a happens-before edge (`WaitGroup.Wait`, `Mutex.Lock`, `RWMutex.Lock` and `RLock`, their `TryLock` forms, `Cond.Wait`, `Once.Do`, `sync.Map`'s read operations, `sync.Pool.Get`, and every `sync/atomic` operation), a call whose summary may synchronise in any package, such as `errgroup.Group.Wait`, a call of an interface method or a function value, and a function's deferred calls where it returns. A release, such as `Mutex.Unlock` or `WaitGroup.Done`, is not. Releases are not paired with acquires, so synchronising on either side keeps a pair silent. An access is a load or a store of the variable or of a field of a struct variable, so `s.a` and `s.b` are told apart; a variable a loop body declares is a new one each iteration. The variable's address passed on, a write through a pointer it holds, a variable captured only by a function literal nested in the goroutine, `go f(&x)`, and two goroutines racing with each other are not followed. | `go func(){ err = f() }(); if err != nil {…}` |
| MS402 | A map written in a goroutine and accessed concurrently without synchronisation | — |

### Tier 1 + annotations — Sync

| ID | Check | Annotation |
|---|---|---|
| MS403 | A field accessed without holding its guard lock, or a function called without the lock it requires | checklocks syntax ([D6](decisions.md)): `// +checklocks:mu` on the field; `+checklocks:mu`, `+checklocksread:mu`, `+checklocksacquire:mu`, `+checklocksrelease:mu` on functions |

### Tier 2 — Ownership and borrowing (fires only on annotated code; sound within that scope)

| ID | Check | Example |
|---|---|---|
| MS501 | Use after move: a value is used after its ownership moved. A value of an `//memsafe:owned` type, or a pointer to one, moves when it is sent on a channel (in a `select`, only when its case runs) or stored into a field, element, or map entry of another owned value, including in a composite literal; any value moves when it is passed to a `//memsafe:consumes` parameter, by a call or a `go` statement, not by a `defer`, which takes it only at the return. As in Rust, a use is reported when the value moved on some path to it, not only on every path, unless the variable was assigned again since. Conversions to other types and interfaces, phis, and the local variable a moved value was loaded from are followed. A copy kept in memory before the move, a variable captured by a function literal, a global, and calls of interface methods or function values are not. A comparison with `nil` is no use. | `out <- f; f.data[0] = 1` |
| MS502 | A borrowed parameter escapes the call | `//memsafe:borrows p` then `h.raw = p` |
| MS503 | `append` aliasing: a slice is reused after `append` on a shared prefix of the same backing array. Applies to owned or borrowed slices; enable `-ownership.appendalias` to check every slice. | `b := append(a[:i], x); use(a)` |

### Tier 3 — Strict package mode

A package containing `//memsafe:strict` gets the equivalent of `#![forbid(unsafe_code)]`
plus discipline rules:

- MS901: importing `unsafe` or `"C"` is forbidden.
- MS902: every goroutine-shared mutable variable must be guarded (`+checklocks`), owned, or
  provably synchronised.
- MS903: `//memsafe:allow` must name a specific check and give a reason.

## 6. Annotation language

The rationale is in [D4](decisions.md). Directives
take the form `//memsafe:<verb> [args]` with no space after `//`, following the `//go:`
convention, so `gofmt` and godoc leave them alone. Lock and atomic contracts are the one
exception: they use gVisor checklocks' annotations verbatim
([D6](decisions.md)), so existing annotated
code works unchanged.

| Scope | Directive | Meaning |
|---|---|---|
| Package | `//memsafe:strict` | Enables Tier 3 |
| Package | `//memsafe:forbid unsafe,cgo` | A narrower form of strict |
| Type | `//memsafe:owned` | Values have move semantics: a send on a channel, or a store into another owner, moves them (MS501) |
| Field | `// +checklocks:<lock>` *(checklocks syntax)* | Access requires holding the named lock |
| Func | `//memsafe:consumes <param>` | Ownership of the argument moves to the callee: the caller must not use it again (MS501) |
| Func | `//memsafe:borrows <param>` | The parameter must not escape the call |
| Func | `//memsafe:viewof <param>` | The first result is a view into the parameter, valid until a call ends the parameter's views: one that `invalidates` declares, or any call the standard library documents as ending views of it (MS302) |
| Func | `//memsafe:invalidates <param>` | A call ends every outstanding view of the parameter (MS302) |
| Func | `// +checklocks:<lock>` / `+checklocksread` / `+checklocksacquire` / `+checklocksrelease` / `+checklocksexclude` *(checklocks syntax)* | Lock contract |
| Line | `//memsafe:allow MS301[,MS302] <reason>` | Suppression on the same or the next line. The reason (rest of the line) is mandatory. |
| Line | `// SAFETY: <why>` | Justifies the `unsafe` operations of the statement on the next line (MS104). The reason is mandatory. |

```go
// Buffer is a reusable byte buffer drawn from a pool.
//
//memsafe:owned
type Buffer struct{ b []byte }

// Bytes returns a view that is valid until the next Reset or Write.
//
//memsafe:viewof b
func (b *Buffer) Bytes() []byte { return b.b }

//memsafe:invalidates b
func (b *Buffer) Reset() { b.b = b.b[:0] }

type Cache struct {
	mu sync.Mutex
	// +checklocks:mu
	items map[string]*entry
}
```

### Diagnostics

```
server.go:42:3: MS301: buf used after it was returned to sync.Pool
    buf.WriteString(p)
    ^^^
  server.go:40:2: buf returned to pool here
    pool.Put(buf)
  help: move pool.Put(buf) after the last use, or use `defer pool.Put(buf)`
```

Each finding is an `analysis.Diagnostic` with `Category` set to its check ID, `Related` for
the secondary locations, and `SuggestedFixes` when the fix is mechanical. The `gomemsafe`
front-end renders this rich format from vet's `-json` output. Plain `go vet` users see the
standard one-line form, and golangci-lint and gopls show the same diagnostics natively.

## 7. Performance design

Speed is a first-class requirement, not an afterthought. The rationale is in
[D1](decisions.md) and
[D2](decisions.md).

### 7.1 Budgets (targets; validated and fixed during M0)

| Scenario | Target |
|---|---|
| Warm re-run, nothing changed | ≤ the time `go vet ./...` takes on the same tree: every package should be a cache hit |
| Edit one package's function bodies without changing its summaries | That package is re-analysed in ≤ 300 ms for a 10 kLOC package. Its facts file stays byte-identical, so the facts never re-run a dependent; the go command still re-runs dependents whose compile inputs changed (see §11) |
| Cold run, including dependency facts | ≤ 2× the time of `go vet ./...` with the default vet suite |
| Peak RSS per package process | ≤ 2× the `compile` process for the same package |
| Per-function analysis | Bounded (§7.3); no input may cause super-linear blow-up |

### 7.2 Where the speed comes from

1. **The go command does the scheduling and the caching.** `cmd/go` runs a vet tool once per
   package, in parallel, in dependency order. It caches each run under a key built from the
   tool's ID, the flags, the package's action ID, and the **content hash of each
   dependency's `.vetx` facts file**. Dependencies run in facts-only (`VetxOnly`) mode. We
   inherit all of this at no cost.
2. **Deterministic, minimal facts.** Because dependents are keyed on the *bytes* of a
   dependency's `.vetx` file, our facts must be byte-identical whenever the summaries are
   unchanged, and a test enforces it. Dependents are also keyed on their own compile action,
   which the go command changes when a dependency's compiled export data changes (§11), so
   some body edits re-run dependents regardless of facts. We encode facts
   compactly (bitsets, sorted keys), and an empty summary emits no fact at all. Fact churn is
   treated as a performance bug, and a test enforces this.
3. **Dependencies pay only for summaries.** `summary` is the only analyzer that exports
   facts. In `VetxOnly` mode, though, unitchecker keeps every analyzer that *transitively
   requires* a fact producer, and every check requires `summary`. To stop the checks from
   running on every dependency and having their output discarded, `cmd/gomemsafe` reads
   `VetxOnly` from the `vet.cfg` before calling `unitchecker.Main`, and each check's `Run`
   returns immediately in that mode. Removing the checks from the analyzer list is not an
   option: the go command passes the same analyzer flags to every run, and unknown flags fail
   to parse. The standard library and third-party dependencies are therefore summarised once
   per toolchain version, and nothing more.
4. **No whole-program analysis.** We do not use `go/pointer` (deprecated and whole-program),
   whole-program call graphs, or VTA. Interprocedural knowledge comes only from summaries.
5. **Prefilter before SSA, and build SSA lazily.** Most functions in most packages contain no
   trigger sites. One cheap AST pass decides which functions enter dataflow at all. SSA
   comes from an internal `lazyssa` analyzer whose result is a build-once function, rather
   than from `buildssa`, which always builds and pulls in `ctrlflow`. A package is lowered to
   SSA only if a check found a trigger, or if `summary` found a function whose
   parameters, receiver, or results contain pointers. SSA is built per package, never
   per function, and the summary and the checks share one build.
6. **Sparse tracking.** Dataflow tracks only values born at a *source*: a pool `Get`, a
   borrow-returning call, an owned value, a cgo allocation, a goroutine capture. Functions
   with zero tracked values return at once.
7. **Allocation discipline in the engine.** Values get dense integer IDs, the bitset state is
   reused between functions, the hot loops contain no maps, and profiling decides any further
   tuning.
8. **A benchmark in CI.** A small pinned corpus, two real libraries (`go.uber.org/zap`, which
   uses `sync.Pool` throughout, and the GoAkt actor framework, a larger module with a deep
   dependency graph) and a generated corpus, is measured on every build by
   `internal/cmd/bench`. Once the first runs give a stable baseline, a regression of more
   than 5% in wall time or RSS fails the build.

### 7.3 Fail quiet, never slow

Each function's dataflow has an instruction × iteration budget. If the budget is exceeded,
the function's result becomes *unknown*. An unknown result produces **no diagnostics** (and
a conservative summary). It never produces a slow run or a speculative report. The `-stats`
front-end flag reports how often budgets were hit, so precision losses stay visible.

## 8. Integration

```sh
# Pin the version per module (Go 1.24+ tool directive)
go get -tool github.com/tochemey/gomemsafe/cmd/gomemsafe
go tool gomemsafe ./...                      # rich output, non-zero exit on findings

# Or as a plain vet tool
go vet -vettool=$(go tool -n gomemsafe) ./...

# Select analyzers with standard vet flags: naming some runs only those...
go tool gomemsafe -lifetime -sharing ./...
# ...and disabling some runs all the others
go tool gomemsafe -cgomem=false ./...

# Analyzer-scoped flags turn on opt-in sub-checks
go tool gomemsafe -unsafeflow.safety ./...

# Output format (front-end only; not passed to vet)
go tool gomemsafe -json ./...
go tool gomemsafe -sarif ./... > memsafe.sarif
```

Flag behaviour comes from `go/analysis`'s standard flag handling, so it matches `go vet`
exactly: every check is on unless analyzer flags are given, `-<name>` selects, and
`-<name>=false` disables. The `go vet -vettool` form accepts the same analyzer flags.

- **Library:** `import "github.com/tochemey/gomemsafe/analyzers"`, then pass
  `analyzers.Default()` to `multichecker.Main` or any other `go/analysis` driver.
- **golangci-lint:** distributed as a module plugin (`custom-gcl`); gopls can load the same
  analyzers.
- **A build-gating `-toolexec` mode is out of scope for v1** (D1).

## 9. Known limitations

These are stated plainly so that users know what a clean run does *not* prove.

- Outside annotated scope, the analysis is unsound by design. It can miss hazards that flow
  through interfaces, function values, reflection, package-level globals mutated from several
  packages, or assembly.
- Happens-before reasoning is intra-procedural plus summaries, and a release is never paired
  with its acquire: any synchronisation on either side of a goroutine capture keeps MS401
  silent, so a race that merely sits next to a lock or a channel operation is missed.
- Generic functions are summarised once over their generic body, with type parameters treated
  conservatively.
- MS301 tracks only values that share memory with the callee: pointers, slices (reslices
  included), maps, channels, functions, unsafe pointers, and interfaces. Values the call copies
  (numbers, strings, structs and arrays passed by value) cannot be invalidated and are not
  tracked. A value of a type parameter is summarised for callers, which decide from their
  concrete argument, but its uses inside the generic body are not reported. Values loaded
  through memory and calls through method values are not tracked, so uses of them are missed
  rather than reported.
- The C side of cgo is invisible to the analyser.
- gomemsafe complements `-race`, `go vet`, staticcheck, and nilaway. It does not replace them.

## 10. Milestones

| Milestone | Scope | Exit criteria |
|---|---|---|
| **M0 — Spike** | Dual-mode binary with the `VetxOnly` short-circuit; `directive`, `prefilter`, `lazyssa`, `flow`, a minimal `summary`; MS301 end to end; benchmark harness in CI | Budgets in §7.1 measured; the SSA share in §11 evaluated; decisions.md updated if a decision changes |
| **M1 — Unsafe, cgo, lifetimes** | MS101–MS104, MS201–MS203, MS301–MS303, known effects for the standard library | Zero false positives on the corpus at default settings, or each one triaged and fixed |
| **M2 — Summaries and sharing** | `summary` facts, MS401–MS403 (checklocks-compatible) | Determinism test for facts; warm-edit budget met; checklocks conformance run in CI |
| **M3 — Ownership** | MS501–MS503, interface contracts | Sound on an annotated test corpus (mutation-tested) |
| **M4 — Polish** | Strict mode, golangci-lint module plugin, SARIF, docs site | First tagged release |

## 11. Facts the design relies on

Each of these was checked against the source of Go 1.27.2 and x/tools v0.52.0.

| Fact | Consequence for the design |
|---|---|
| `ssa.Package.Build` builds a whole package at a time; there is no per-function build. | SSA is built lazily per package (§7.2 item 5). If M0 shows SSA construction above 40% of cold-run time **and** the cold-run budget in §7.1 is missed, dataflow moves to per-function CFGs from `x/tools/go/cfg`, and [decisions.md](decisions.md) is updated. |
| With `-json`, unitchecker exits 0 even when it has findings, and the go command caches every run that exits 0. | The front-end always uses `-json` and sets the exit code itself, so runs with findings are cached too. JSON `"error"` entries count as failures. |
| In `VetxOnly` runs, unitchecker also runs every analyzer that transitively requires a fact producer. | Checks return immediately in `VetxOnly` runs (§7.2 item 3). |
| The go command's vet cache key covers the tool ID, the flags, the package's inputs, and its dependencies' facts, and nothing else. | No config file ([D7](decisions.md)); facts must be byte-deterministic (§7.2 item 2). |
| The go command keys a vet run on the package's compile action ID as well as on its dependencies' `.vetx` bytes. A body edit that changes a package's compiled export data changes its dependents' compile action IDs. | Dependents of an edited package may be re-analysed even though its `.vetx` file is byte-identical (verified: identical 1,577-byte facts file across a body edit, dependent still re-run). Our share of the cost is the facts, and they are deterministic. |
| Standard-library summaries would differ per Go release, GOOS/GOARCH, and build tags. | Summaries are computed and cached in GOCACHE, never shipped. `internal/effect` holds only behaviour the code can't show, keyed by full object path (`sync.(*Pool).Put`) and tested on every supported Go release. |

**Measured in M0:** the SSA share above, and every budget in §7.1.
