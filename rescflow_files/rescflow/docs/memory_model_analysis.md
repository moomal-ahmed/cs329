# Was the memory/resource model preserved? (cases whose output matched Go)

This covers every case where the translated program's output matched Go's:
- **Rustygo**, as emitted: all 20 cases (23 inputs).
- **Go2Rust**, with the two stand-in patches only (`results/go2rust_patched.json`): cases
  01–07, 09–15, 18 and 20. Unpatched Go2Rust compiled nothing, so it has no matching case.

Matching output is necessary but not sufficient, so each case was also checked three other ways:
1. **Generated code**: the translated `scenario` function, read by hand
   (`results/baseline_artifacts/*/`).
2. **Identity probe** (Go2Rust): which object each `Close()` actually acted on
   (`go2rust_patched.json`, field `identity`).
3. **OS trace**: `strace` of the real `openat`/`close` calls on the input file, interleaved
   with the program's printed events, for Go, Rustygo and Go2Rust (`results/os_traces.json`,
   produced by `scripts/os_trace.py`).

## Properties checked

| # | Property | What Go does |
|---|---|---|
| P1 | Handle identity and aliasing | `*os.File` is a pointer; all copies refer to one object |
| P2 | Release timing and order | Deferred calls run at **function** exit, in LIFO order, only if the `defer` was reached |
| P3 | Capture semantics | Arguments of `defer f(x)` are evaluated at registration; a closure reads variables when it runs |
| P4 | Lifetime and escape | A returned or stored handle outlives the function; memory is reclaimed by the GC |
| P5 | Real resource state | The OS descriptor is held from `Open` until `Close`; `Close` reports real errors; use after close fails |
| P6 | Exceptional paths | Deferred calls also run while a panic unwinds, and `recover` can stop it |

## Summary

| | P1 | P2 | P3 | P4 | P5 | P6 |
|---|---|---|---|---|---|---|
| **Rustygo** (all 20) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Go2Rust** (16 matching) | ✅ | ✅ | ⚠️ correct for arguments, wrong for closures (case 14) | ✅ | ❌ in every case | ❌ latent in every case |

**Rustygo preserves Go's memory model by emulating it.** Heap objects are `Ptr<…>` values
managed by Rustygo's garbage collector, with explicit GC root frames. `defer` becomes a
runtime `Defers` stack of heap records whose fields hold the already-evaluated arguments.
Every function with defers is wrapped in `catch_unwind`. `os.File` is Go's real `os` package
compiled to Rust. The OS trace matches Go's for all 23 runs: the same `openat`/`close` calls,
in the same position relative to printed events, with nothing open at exit. Rust's ownership
system (`Drop`, moves, borrows) is not used to manage these resources at all. The behaviour
is Go's, re-implemented in Rust.

**Go2Rust preserves the program-level bookkeeping, but not the resource.** Pointers become
`Rc<RefCell<Option<T>>>`, so aliasing (P1) and escape (P4) work through reference counting
instead of a GC. `defer` becomes a per-function `Vec<Box<dyn FnOnce()>>` that is popped (LIFO)
before every `return` the translator emits (P2). Every value a defer mentions is snapshotted
with `Rc::clone` at registration. That is right for deferred-call arguments, but wrong for
closures that should see later reassignment (P3, case 14). The resource itself is not
modelled (P5): `os.Open` is `std::fs::read`, which reads the whole file into a `Vec<u8>` and
closes the OS descriptor before returning. The OS trace shows `[os open] [os close]` before
`acquire A true` in every case that opens a file, so the file is never open while the program
uses it. `os_File::close()` only sets an `AtomicBool` and always returns `nil`. There is no
`catch_unwind` (P6), so any panic would skip all deferred cleanup. Case 17 shows this, and it
is a latent risk in every case below.

## Per-case results

Legend: **Preserved**: all six properties hold. **Partial**: program-level behaviour (P1–P4)
holds, but the resource (P5) and/or panic path (P6) do not. **Not preserved**: the output
matches, but the wrong resource is acted on.

| # | Case | Go behaviour being tested | Rustygo | Go2Rust (patched) |
|---|---|---|---|---|
| 01 | local | Open A, `defer Close(A)`, use, close at exit | **Preserved.** One deferred record `{label, f}`; `Defers.run()` before return; OS close after `use A` | **Partial.** Rc clone captured at registration; stack drained before return. OS descriptor already closed inside `Open` |
| 02 | early_before | Return before `defer` is registered → nothing to close; otherwise as 01 | **Preserved.** Early path never pushes a record; normal path as 01 | `early` input: **Preserved** (no file opened, so nothing to model). Normal input: **Partial**, as 01 |
| 03 | early_after | Early `return` after `defer` must still close A | **Preserved.** `__defers.run()` emitted on the early-return edge; OS close after `early` | **Partial.** Translator inserts a drain loop before the early `return`; OS close still inside `Open` |
| 04 | failed_acquire | `Open` fails, so no `defer`, no close | **Preserved.** Real `openat` fails; no record pushed | **Preserved.** `std::fs::read` fails; nothing registered; no OS handle in either |
| 05 | two_lifo | Close B before A | **Preserved.** Two records, run LIFO; OS closes #2 then #1, matching Go | **Partial.** `Vec::pop` gives LIFO, so the order is right. Both OS descriptors closed during their own `Open` |
| 06 | nested_block | Defer inside `{}` runs at **function** exit, not block exit | **Preserved.** SSA removes the block; record runs at function exit; OS close after `after-block` | **Partial.** Defer stack belongs to the function, and the closure's Rc keeps A alive past the Rust block. Timing is right; no OS handle held |
| 07 | conditional | Defer registered only when `mode=="defer"`; otherwise explicit close | **Preserved** (both inputs). Push is inside the branch; explicit close otherwise | **Partial** (both inputs). `push` is inside the `if`, so registration is dynamic, as in Go |
| 09 | return_open | Handle escapes by return; caller uses and closes it | **Preserved.** GC pointer returned; OS descriptor stays open across `returned` until the caller's close | **Partial.** Rc returned to caller (refcount keeps it alive); identity probe: caller closes A. No OS handle held across the return |
| 10 | return_closed | Defer closes A, then a closed handle is returned; caller's `use` fails | **Preserved.** Return value stored in the result slot **before** `Defers.run()`, as in Go; OS close before `returned`; caller's `Stat` fails for real | **Partial.** Correct trace, but the return expression `f.clone()` is evaluated **after** the defers run, the reverse of Go's order. It is harmless here because no defer changes `f`. `use A false` comes from Go2Rust's closed flag via the patched `stat` |
| 11 | global_escape | Handle stored in a global; closed in `main` | **Preserved.** Stored into a global slot that is a GC root; OS descriptor open across `stored`/`returned` | **Partial.** Rc stored in the global cell; identity probe: `main` closes A |
| 12 | alias | `g := f; defer Close(g); use(f)`: one object, two names | **Preserved.** SSA makes `g` and `f` the same value `t2` | **Partial.** `g = f.clone()` is an Rc clone, so it shares the same `os_File`; probe: one close of A |
| 13 | eager_argument | `defer Close(f)` saves A; later `f = g` must not change it | **Preserved.** Record stores `t2` (A) at registration; B closed explicitly; OS #2 then #1, as in Go | **Partial.** Snapshot `f_defer_captured = f.clone()` at registration; `f = g` rebinds the local, so the defer still closes A. Probe: no wrong-object close |
| 14 | late_closure | Closure reads `f` **at exit**, after `f = g`, so it must close B | **Preserved.** `f` is moved into a shared heap `Slot`; the closure reads the slot at exit and closes B. OS trace matches Go | **Not preserved.** The closure gets the same registration-time snapshot as case 13, so it closes **A again** (already closed) and **B is never closed**. Output matches only because `close()` cannot fail and the label is a fixed string |
| 15 | closure_parameter | `defer func(x){…}(f)`: parameter fixed at registration | **Preserved.** Record holds the evaluated argument `t2` (A) | **Partial.** Explicit `__defer_arg_0 = f.clone()` passed to the closure: correct eager semantics |
| 18 | helper_boundary | Defer inside `helper(f)` closes A when the helper returns; caller's `use` then fails | **Preserved.** Separate `Defers` in `helper`; OS close after `helper`, before the caller's use | **Partial.** Helper has its own defer stack, drained at helper exit. `use A false` relies on the closed flag via the patched `stat` |
| 20 | interface_dispatch | `defer c.Close()` through an interface wrapping `f` | **Preserved.** Interface value saved in the record; dynamic dispatch through the method table at exit | **Partial.** `Box<dyn closer>` in an Rc cell; `tracked.f` is an Rc clone of A. Untested: reassigning `c` after the `defer` (Go fixes the receiver at registration) |

Cases 08, 16, 17 and 19 are not listed for Go2Rust because their output did not match.
For Rustygo they are also **Preserved**:
- 08: three heap records, one per loop iteration, closed in order 2, 1, 0.
- 16: the deferred closure writes the named result through a heap slot after `return nil`,
  and the real second `Close` error reaches it.
- 17: `catch_unwind` runs `Close(A)`, then the recover closure, then returns normally.
- 19: the second `Close` returns Go's real "file already closed" error.

## Why the two tools behave this way

- **Rustygo compiles Go's SSA, not Go syntax.** Defer, panic and recover, closures over
  mutable variables, and the heap are implemented as runtime services. These are the same
  services the Go runtime provides, so Go's semantics are preserved by construction,
  including OS-level resource behaviour. The price is that nothing is expressed in Rust's
  ownership model: no `Drop`, no borrowing, a GC and runtime in every binary, and about
  4.5 MB of generated Rust per benchmark.
- **Go2Rust translates syntax.** It makes each local an `Rc` cell, each defer a boxed closure,
  and each standard-library call a call into a hand-written stand-in. One snapshot rule
  ("clone every referenced variable at `defer`") is correct for the common forms (01–13, 15,
  18, 20) but wrong for closures over reassigned variables (14). Because the `os` stand-in
  does not hold a real handle, the resource model (P5) cannot be preserved in any case, and
  the traces cannot reveal this.
- **Neither tool replaces `defer` with Rust ownership-based cleanup.** Both keep an explicit
  runtime list of pending cleanups. This is evidence for the RescFlow hypothesis that mapping
  `defer` to `Drop` needs analysis neither tool performs: function-exit vs scope-exit timing
  (06), loop instances (08), conditional registration (07), capture semantics (13–15), escape
  (09–11) and observable `Close` errors (16, 19).

## Limits of this analysis

- Go2Rust results use two evaluator patches to its `os` stand-in. The `stat` patch reads
  Go2Rust's own closed flag, which the `use A false` events in cases 10 and 18 depend on.
- The benchmarks print fixed labels, so the identity probe (Go2Rust) and the real `Close`
  errors (Rustygo) are what guard against wrong-object closes. Without them, case 14 would
  look correct.
- The OS trace covers descriptors for the input file only, not heap memory. Heap reclamation
  (Go GC vs Rustygo GC vs Go2Rust reference counting) was not measured. None of these
  programs creates reference cycles, where Rc would leak and a GC would not.
- One input file, sequential code only, and one revision of each tool.

## Addendum (2026-11-23): LLM-written translations (`Claude_translations/`)

18 files were committed to `main` (cases 05 and 13 are missing). Each was checked with
`python3 scripts/eval_llm_translations.py <go1.26 GOROOT>` (`results/llm_translations.json`):

| Check | Result |
|---|---|
| Compiles (`rustc --edition 2021 -O`) | 18/18, 0 warnings |
| Output matches Go on the labels.json inputs | 21/21 |
| OS open/close trace identical to Go (P5) | 21/21, no handle open at exit |
| Extra inputs (missing path, no arguments) match Go | 36/36 |

How each case represents the file:

| Representation | Cases |
|---|---|
| Plain owned `File`; `defer` → `Drop` guard (`DeferClose`), `DeferStack` for loops, `Option` for the conditional defer | 01, 02, 03, 04, 06, 07, 08, 09, 12, 15, 17 |
| Owned file moved into a global `static Mutex<Option<File>>` (Rust requires synchronized statics) | 11 |
| `RefCell<Option<File>>` cell read by the deferred closure at exit (late capture) | 14 |
| Runtime-checked shared handle `OsFile` (use-after-close / double close kept as Go behaves) | 10, 16, 18, 19, 20 |

`unsafe` is used only for the FFI declaration and call of `close(2)` on the fd taken out of
the `File` with `into_raw_fd`, so each fd is closed exactly once, and the close error is visible.

Caveats: these translations were produced with the labels and design categories available,
so they are a strong reference, not an unbiased baseline. Case 20 may not need `OsFile`,
because the program never uses the file after closing it.
