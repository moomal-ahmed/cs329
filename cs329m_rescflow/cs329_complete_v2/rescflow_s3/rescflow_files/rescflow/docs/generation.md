# Step 5: deterministic Rust generation

`rescflow -emit-rust DIR <packages>` writes one Cargo project per program
(`DIR/<program>/{Cargo.toml, src/main.rs}`, depending on `rescflow_rt` by relative path).
Category C programs get their step-6 `REVIEW.md` instead. Programs outside the generator's subset
get `UNSUPPORTED.txt` with the exact construct and line. No LLM is involved: the same input always
produces the same Rust (`internal/analysis/gen.go`).

```bash
./rescflow -emit-rust generated/benchmarks ./benchmarks/...
python3 scripts/check_generated.py ./rescflow /path/to/go1.26.5        # results/generated_check.txt
```

## How a program is generated

**Resource cleanup comes from each `defer`'s facts**, the same facts the step-3 rules decide from:

| Defer facts | Form emitted | rescflow_rt |
|---|---|---|
| top-level, arguments fixed at registration | `OWNED_GUARD` | `let _d = DeferOwn::new(file, move \|f\| cleanup(args, f));` |
| inside a plain nested block | `OWNED_GUARD`, hoisted | `let _d;` before the enclosing statement, assigned at the `defer` |
| on one branch only | `OPTION_GUARD` | `let mut _d = None; … _d = Some(Defer::new(..))`; the file stays in its cell until exit |
| inside a loop | `DEFER_STACK` | `DeferStack::push_file`; later uses read the file through its slot |
| closure reading variables at exit | `CELL_CLOSURE_GUARD` | `Defer::new(\|\| ..)` over `RefCell` variables |
| closure writing a named result | `RESULT_SLOT_GUARD` | the results are cells; the body is a labelled block, so guards run before the result is read |
| `defer func(){ if recover() != nil {..} }()` | `CATCH_UNWIND` | the rest of the body runs in `recover(\|\| ..)` |
| returned / stored in a global / out-parameter | ownership move | `Option<File>` result, `static Mutex<Option<File>>`, `&RefCell<Option<File>>` |

**Go values.** A `*os.File` is `Option<File>` (nil-able); `error` is `go::Error`. `os.Open`, `Close`,
`Stat` and `os.Args` map to `rescflow_rt::go` shims. `Close` and `Stat` on nil fail as in Go, and an
unrecovered `panic` prints `panic: msg` and exits with 2.

**Where each file lives.** The generator tracks each resource *object* to its current place: a variable,
guard, struct field, cell, global, or defer-stack slot. That resolves aliases statically (`g := f` reads
through `f`'s current place) and handles moves into guards and struct fields. A parameter is taken by
value (`Option<File>`) when the callee's summary says it releases or keeps it, and borrowed
(`Option<&File>`) otherwise. A variable becomes a `RefCell` cell only when it is captured by a closure, has its
address taken, or is deferred conditionally.

**The non-resource code** is translated directly: strings, ints, bools, slices of strings, `if`/`for`
(with Go's `continue` semantics), struct types and methods, package-level variables (`static Mutex`),
`fmt.Println`, `fmt.Sprint`, `len`, `panic`.

## Results (`results/generated_check.txt`)

Every generated program is built against `rescflow_rt` and run next to the Go original on:
every labelled input; for each mode, "the k-th open fails" for every k and "the first close fails";
a missing file; and no arguments. Compared: stdout, exit code, first stderr line, and the OS-level
open/close order (strace).

| Set | A/B programs | Generated | Identical to Go | Review only (C) | Unsupported |
|---|---|---|---|---|---|
| Benchmarks (20) | 16 | 15 | **71/71 runs** (15 programs) | 4 (10, 16, 18, 19) | 1 (20) |
| Held-out (14) | 12 | 12 | **76/76 runs** (12 programs) | 2 (H13, H14) | 0 |

Forms emitted, by program: every plain defer `OWNED_GUARD` (06 hoisted); 07 `OPTION_GUARD`; 08, H01, H02, H03, H07
`DEFER_STACK`; 14 and H04 `CELL_CLOSURE_GUARD`; H10 `RESULT_SLOT_GUARD`; 17 and H11 `CATCH_UNWIND`; 09, 11, H08 and H12
ownership moves.

**Unsupported: benchmark 20** (`interface_dispatch`). Go interfaces are outside the generator's subset, and the
program keeps using the file after it moves into the interface value. An ownership-based Rust form needs either
an interface method that exposes the file or a shared handle. Rules say A (`DYN_GUARD`), generator says
unsupported: this is reported, not hidden.

## Limits

- **The subset is the corpus's:** no `range`, `switch`, maps, channels, goroutines, generics, interfaces;
  `fmt.Println`/`Sprint` only.
- `old := f` from a cell variable *moves* the file out of the cell. That is correct when the variable is
  reassigned before it is read again (case 14); otherwise it would differ.
- Overwriting an `Option<File>` that still holds a file would close it (Rust drops it). Go would leak it
  instead. No A/B program does this.
- Strings are `String` everywhere (`.clone()` at each use): correct, not idiomatic.
- The sealed set `testdata/final_unseen` was unsealed and run once for the final evaluation
  (2026-11-23): 5 of 10 generated, 28/28 runs identical to Go, 3/3 true C programs sent to review, and
  U03/U04 conservatively sent to review as false C. See `docs/final_evaluation.md`.
