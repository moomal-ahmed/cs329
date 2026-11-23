# Go2Rust vs Rustygo on the 20 RescFlow benchmarks

Run 2026-11-09. All numbers below come from executed runs saved under `results/`.

## Setup

| Item | Value |
|---|---|
| Go reference | Go 1.27.0, `results/go_runs.json`: 20/20 programs, 23/23 inputs match the labels. Rechecked on Go 1.26.5 with identical output (`results/go_runs_go126.json`) |
| Go2Rust | `tylerlaprade/go2rust` @ `3e81880e31cb589e44c5ebb57dc135a87a216515`, built with `go build ./go` |
| Rustygo | `KarpelesLab/rustygo` @ `cf2f583dce0557862fdf3bd7fb46789ee06260f1`, built with `go build ./cmd/rustygo` |
| Toolchains | Go 1.26.5 (standalone GOROOT, required by Rustygo), rustc/cargo 1.97.0 |
| Input | one file containing `resource fixture\n`; modes as in `benchmarks/labels.json` |
| Match criterion | translated stdout and exit code identical to observed Go output for the same input |

Each benchmark was copied unchanged into a one-file module that declares `go 1.26`, because
Rustygo's standard-library overlays are written for Go 1.26. Commands:
`python3 scripts/run_baselines.py ...` (both tools, as emitted) and
`python3 scripts/run_go2rust_patched.py` (secondary Go2Rust experiment, explained below).

## Headline results

| | Translated | Compiles | Input pairs matching Go | Cases fully matching |
|---|---|---|---|---|
| **Rustygo** (as emitted) | 20/20 | 20/20 | **23/23** | **20/20** |
| **Go2Rust** (as emitted) | 20/20 | **0/20** | 0/23 (none ran) | 0/20 |
| Go2Rust + 2 stand-in patches (secondary) | 20/20 | 20/20 | 19/23 | 16/20 (15/20 after the identity probe) |

**Go2Rust as emitted.** Every output fails to compile with the same two errors. Both are in
Go2Rust's generated `os` stand-in, not in translated benchmark code:
- `E0530`: the stand-in `os::open` declares `let path`, which collides with the benchmarks'
  global `var path`.
- `E0599`: the stand-in `os_File` has no `stat` method, and the `useFile` helper calls `f.Stat()`.

These are real translator defects, so the primary Go2Rust result is 0/20 compiled.

**Secondary experiment.** To measure Go2Rust's `defer` and resource handling on its own,
`run_go2rust_patched.py` renames the stand-in's local variable and adds a `stat` method that
fails only if the stand-in's own closed flag is set. No translated scenario or helper code is
changed, and `Close()` is left as Go2Rust wrote it. The patched files are saved as
`main.patched.rs` next to each original `main.rs`. These numbers are not Go2Rust's own
success rate.

**Identity probe (Go2Rust only).** The benchmarks print a fixed label (`close B`) whatever
object is actually closed, and Go2Rust's `Close()` always returns success. A wrong-object close
can therefore print the correct trace. A separate probe build logs the object each `Open` and
`Close` touches; the match verdict above does not use it. Rustygo does not need the probe:
it compiles Go's real `os` package, so a wrong-object or repeated close shows up as
`close X false`, as it does in Go for cases 16 and 19.

## Per-case results

| # | Pattern | Rustygo | Go2Rust (patched) | Go2Rust problem |
|---|---|---|---|---|
| 01 | local | ✅ | ✅ | |
| 02 | early_before (2 inputs) | ✅✅ | ✅✅ | |
| 03 | early_after (2 inputs) | ✅✅ | ✅✅ | |
| 04 | failed_acquire | ✅ | ✅ | |
| 05 | two_lifo | ✅ | ✅ | |
| 06 | nested_block | ✅ | ✅ | |
| 07 | conditional (2 inputs) | ✅✅ | ✅✅ | |
| 08 | loop | ✅ | ❌ crash | `fmt.Sprint` is an unimplemented stub ("sprint bridge: generic stub function body has no implementation"); panics before any output. Not a defer issue |
| 09 | return_open | ✅ | ✅ | |
| 10 | return_closed | ✅ | ✅ | |
| 11 | global_escape | ✅ | ✅ | |
| 12 | alias | ✅ | ✅ | |
| 13 | eager_argument | ✅ | ✅ | |
| 14 | late_closure | ✅ | ⚠️ trace matches, behaviour wrong | Closure captures `f` by cloning its `Rc` at `defer` time, and `f = g` rebinds the local instead of writing through the shared cell. The deferred call closes **A a second time**; **B is never closed**. Hidden because `Close()` never fails |
| 15 | closure_parameter | ✅ | ✅ | |
| 16 | named_result | ✅ | ❌ | The named-result write-back is translated correctly (shared `result` cell, defers run after `return nil`). The second `Close()` returns success instead of an error, so `result-error` is `false` |
| 17 | panic_unwind | ✅ | ❌ | Defers run only on normal `return` paths: there is no `catch_unwind` and `recover()` has no effect. The process aborts; `close A` and `recovered` never run, so the file is never closed |
| 18 | helper_boundary | ✅ | ✅ | |
| 19 | double_close | ✅ | ❌ | Second `Close()` reports success; Go reports `close A false` |
| 20 | interface_dispatch | ✅ | ✅ | |

## How each tool models memory and cleanup

Neither tool maps Go `defer` cleanup onto Rust ownership (`Drop`/RAII).

**Rustygo: emulates Go's runtime in Rust.** Generated code is SSA-shaped. Heap values live in
`Ptr<…>` objects managed by Rustygo's own garbage collector, with explicit root frames
(`rustygo::gc::Frame`). Each function with defers gets a runtime `Defers` stack. It is run at
every return, and also from `catch_unwind` on panic, with `recover` support. A closure that
captures a variable that is later reassigned gets a shared heap `Slot` (case 14). `os.File` is
Go's real `os` package compiled to Rust, using real file descriptors and real `Close` errors.
This is why it matches Go on every case: Go semantics are reproduced, not re-expressed through
Rust ownership. Cost: about 4.5 MB of generated Rust per benchmark (the translated standard
library) and about 20 s per build.

**Go2Rust: source-to-source with a mock standard library.** Values are wrapped in
`Rc<RefCell<Option<T>>>`. A defer becomes a closure pushed onto a per-function
`Vec<Box<dyn FnOnce()>>`. The stack is popped (LIFO) at each `return` the translator emits;
nothing runs it on unwind. Deferred-call arguments are captured by cloning the `Rc` at
registration, which gives the correct eager-argument semantics (cases 13 and 15). Results
differ when a captured variable is later reassigned (case 14). `os.Open` reads the whole file
into a `Vec<u8>`, and `os_File::close()` only sets an `AtomicBool`: no OS handle is held, and
`Close` cannot fail.

## Mapping to the semantic model's mutants (`models/defer_model.py`)

| Mutant | Rustygo | Go2Rust |
|---|---|---|
| `bad_fifo` (wrong order) | not observed (05, 08) | not observed (05); 08 untestable (crash) |
| `bad_block_exit` (cleanup at block exit) | not observed (06, 08) | not observed (06) |
| `bad_late_argument` | not observed (13, 15) | not observed (13, 15) |
| `bad_eager_closure` | not observed (14) | **observed (14)**: wrong object closed, B never closed |
| `bad_result_snapshot` | not observed (16) | not observed: write-back correct, but `Close` error lost |
| (not modelled) panic-path defers | correct (17) | **missing (17)** |
| (not modelled) `Close` error fidelity | correct (16, 19) | **missing (16, 19)** |

## Caveats

- 20 small, synthetic, author-written programs with one input file. The labels are now
  confirmed by real Go output but have not yet been reviewed by teammates.
- The traces are instrumented, so these are instrumented-equivalence results only.
- The Go2Rust patched numbers depend on two patches made by the evaluators. They are reported
  separately and must not be cited as Go2Rust's success rate.
- Each tool was tested at one pinned revision; both projects change quickly.
- No performance, memory-use or GC measurements were taken.
