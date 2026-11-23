# Step 1: SSA + memory-aware analysis layer

`cmd/rescflow` takes Go packages and outputs, for every resource acquisition site, what the
program does with that resource: who uses it, where it is released, which deferred call
releases it at each exit, where it escapes, and what is wrong or unknown. It is the input
to step 3 (rules → A/B/C).

```bash
export PATH=/path/to/go1.26.5/bin:$PATH GOTOOLCHAIN=local   # Go 1.26.x, x/tools v0.50.0 (go.mod)
go build -o rescflow ./cmd/rescflow
./rescflow ./benchmarks/14_late_closure                         # JSON
./rescflow -format text -explain ./benchmarks/14_late_closure   # readable, with annotated SSA
python3 scripts/evaluate_analysis.py ./rescflow                 # score against labels
```

Exit code 0 means every package was analyzed. Exit code 2 means at least one package could not be loaded or
built. The report explains why.

## Architecture

```
Go source
  │ go/packages (LoadSyntax) + go/types          internal/analysis/load.go
  ▼
typed syntax trees            ── fails? → load-list / load-parse / load-type explanation
  │ go/ssa (ssautil.Packages, GlobalDebug)
  ▼
SSA: one CFG per function     ── fails? → ssa-build explanation
  │ RescFlow memory-aware layer                   internal/analysis/{state,analyze,model}.go
  ▼
resource records + exits + summaries + unresolved explanations   internal/analysis/report.go
```

| File | Role |
|---|---|
| `model.go` | The resource API model: `os.Open/OpenFile/Create/CreateTemp` acquire; `(*os.File).Close` releases; other `*os.File` methods use; `os.Exit`/`log.Fatal*` exit without running defers |
| `state.go` | Abstract state at a program point: memory-cell contents, resource states, defer stack, decided branch conditions |
| `analyze.go` | Dataflow engine, transfer functions, defer resolution at exits, function summaries |
| `load.go` | Loading, SSA construction, driver, failure explanations |
| `report.go` | Output types and the catalogue of "why this cannot be resolved" explanations |

## What the memory-aware layer adds to go/ssa

go/ssa already provides values in SSA form, the CFG, memory cells (`Alloc`), loads and stores, closure
bindings, `Defer`/`RunDefers` and panic recovery blocks. The layer adds:

| State (per program point) | What it answers |
|---|---|
| **Resources** (one per acquisition site; loop sites split into *this iteration* / *earlier iterations*) | which values are files, and whether two names refer to the same file |
| **Cell contents** (`cell t0(f) → {g@11}`) | aliasing, reassignment, closure capture, struct fields, globals |
| **Resource state** (`Open`, `Closed`, `Failed`, `Outer`, as may-sets) refined on success and error branches | use after close, double close, close of a failed open, leaks |
| **Defer stack**: each entry records, per input, either a *value fixed at registration* or a *cell read at exit* | at each exit, which resource every pending defer actually acts on, and in what order |
| **Decided conditions** on the path | infeasible combinations of repeated tests (case 07) are pruned |
| **Function summaries** (computed bottom-up): releases/uses/escapes per input, fresh result plus its success flag, `bool == (err == nil)` helpers, close error returned | behaviour across calls (helpers, factories, methods, defers inside callees) |

The analysis is **path-sensitive up to 16 states per block**. Paths are kept apart, not
merged, so facts such as "the defer was registered on this path" and "the cell holds B on this path"
stay connected. Beyond 16 states, the extra states are merged.

### Example: case 14 (the case Go2Rust translates wrongly)

```
2: if.done
	t7 = make closure scenario$1 [t0]
	defer t7()          ⟹ push defer@10 [f: cell t0(f), read at exit] (always)
	...
4: if.then              (B's open failed)
	rundefers           ⟹ pop defer@10 -> acts on {f@8}
5: if.done
	*t0 = t10           ⟹ cell t0(f) := {g@11}
	closeFile("A", t13) ⟹ release f@8: Open -> Closed
	useFile("B", t15)   ⟹ use g@11 (Open)
	rundefers           ⟹ pop defer@10 -> acts on {g@11}
```

The defer acts on **B** on the normal exit, and on **A** if B's open fails. The full
listings for every program are in `results/analysis_*_explained.txt`.

## Results (`results/analysis_eval.txt`)

| | Benchmarks (20) | Held-out (14) |
|---|---|---|
| Acquisition sites | 24/24, 0 false | 17/17, 0 false |
| Defer sites | 20/20, 0 false | 17/17, 0 false |
| Defer → resource association | 19 exact + 1 path-dependent superset (case 14, correct) | 17/17 exact |
| Expected anomalies (10, 18 use-after-close; 16, 19 double close; H14 leak path) | 20/20 programs | 14/14 programs |
| Expected unresolved (H13 goroutine, H14 two implementations), none elsewhere | 20/20 | 14/14 |

**Honest caveat:** developing this layer exposed bugs on held-out programs H03, H06, H10 and H14,
and they were fixed. So the held-out set is no longer unseen *for the analysis*. It is still
unseen for the translators. Before the final report, write 5–10 more programs that nobody
looks at until the final run.

## When the analysis cannot be built, or a fact cannot be resolved

Every case below is reproduced by a program in `testdata/limits/` (outputs in
`results/analysis_limits.txt`). Each report entry gives: the construct, the **missing fact**,
**why** the analysis cannot obtain it, the **consequence** for translation, and the **human
input** that would resolve it.

### A. The program cannot be parsed or turned into SSA (nothing downstream runs)

| Case | Test | Missing | Why | Human input |
|---|---|---|---|---|
| Syntax error | `syntax_error` | a syntax tree | SSA is built from typed syntax trees | fix the syntax error |
| Type error | `type_error` | complete types | go/ssa only lowers well-typed packages: every call target and method set must be known | fix the type error or supply the dependency |
| Newer Go than the toolchain | `newer_go` (`go 1.99`) | the package at all | the go command and go/types refuse unknown language versions | analyze with a matching toolchain + x/tools |
| Missing module or import, build tags exclude all files | (load-list) | package files | `go list` cannot resolve the package | make `go build` succeed |
| go/ssa panic | (ssa-build) | SSA | usually a language feature newer than the x/tools release | upgrade x/tools |

### B. SSA exists, but a fact needed for translation cannot be determined

| Kind | Test | Construct | Missing fact | Consequence |
|---|---|---|---|---|
| `goroutine` | H13 | resource in a goroutine | happens-before between its cleanup and the caller (WaitGroup/channel semantics are not modelled) | cleanup time unknown; Rust needs `Send` / thread ownership |
| `dispatch-multi` | H14 | interface call with ≥2 implementations that disagree | which type receives the call | on some path the resource is never closed |
| `dispatch-unknown` | (param interface) | interface call with unknown receiver types | the implementations that can run | treated as an escape |
| `dynamic-call` | `dynamic_call`, `closure_escape` | call through a function value | which body runs | treated as an escape |
| `unknown-callee` | `unknown_callee` (`bufio.NewReader(f)`) | resource passed to code without a model | does the callee close, keep or return it? | escape at the call |
| `recursion` | `recursion` | recursive cycle | a finished summary | unknown at the recursive call |
| `collection` | `collection`, `channel_send` | resource in a slice/map/channel | which element holds which resource | shared ownership, unknown order |
| `closure-escape` | `closure_escape` | closure capturing a resource is returned | when it runs | lifetime extends beyond the function |
| `exit-bypass` | `os_exit` | `os.Exit` with a pending defer | none (known: defers are skipped) | cleanup never runs; confirm intent |
| `unsafe` | `unsafe_alias` | `unsafe.Pointer` of a resource | all aliases | ownership facts untrustworthy |

`unknown-callee` is the one that matters most for real code. Any standard-library call that
takes the file (`bufio.NewReader`, `io.Copy`, `json.NewDecoder`) needs a summary in
`model.go` (e.g. "uses input 0, keeps a reference"). Adding those summaries is the main work
needed to analyze real repositories.

### C. Analyzed, but with stated assumptions or imprecision

- **Branch correlation** assumes a global or parameter compared twice has not changed in between.
  This holds in the corpus, where globals are set once in `main`. A write between the tests would make the
  analysis prune a feasible path.
- **Path bound**: more than 16 distinct states at a block are merged, which can produce "possible" anomalies.
- **Function inputs are field-insensitive**: everything reachable from a parameter is one
  symbolic resource, and a struct with two files passed to a helper is not split.
- **Factories**: every call to the same factory returns the same abstract resource. Two calls are
  not told apart unless they are in a loop.
- **Panics** are modelled only for explicit `panic` and for callees that always panic. Runtime
  panics (nil dereference, index out of range) are not exits in the model.
- **Loops**: all instances from earlier iterations are one abstract resource. Cleanup order
  *between* instances comes from the defer stack, not from per-instance tracking.
- Only `*os.File` is modelled. Database transactions, network connections, locks and so on need rows in
  `model.go`.

## What step 3 consumes

Per record: `in_loop`, `in_goroutine`, the `defers` with `registration` (always/conditional/loop)
and `captures` (value vs cell), `resolves` per exit (several resources mean a
path-dependent target), `close_error_to` (named result), `escapes` (returned, global, input
memory, unknown), `anomalies` (use after close, double close, may-be-open), and `notes` linking to
`unresolved`. Per function: `exits` with the defer order. Together these choose among the
Rust patterns: guard, `Option<guard>`, `DeferStack`, cell plus closure guard, result slot, ownership
move, or review report (C).

## Step 2b: predicted open/close order vs real Go runs

`rescflow -traces` adds a **path explorer** that runs after the analysis. Starting from `main`, it walks concrete paths:
- it follows every feasible branch;
- it walks into calls to analyzed functions, so a helper's opens and closes appear at the call;
- it unrolls each CFG edge at most 4 times;
- it gives every loop iteration its own file instance (`f@10#1`, `f@10#2`, …);
- it keeps values per path, so a defer registered in iteration *k* closes iteration *k*'s file.

For each path it records the file events in the order the OS would see them. A close of an
already-closed file is not recorded, because Go makes no system call for it. Each sequence is
then canonicalized by order of opening: `o1 o2 c1 c2` means open the first, open the second,
close the first, close the second.

`scripts/check_traces.py` runs every Go program under `strace` on:
- every labelled input;
- for each mode, "the k-th open of the input fails" for every k, and "the first close fails";
- a missing path, and no arguments.

It records the real `openat`/`close` calls on the input file, canonicalizes them the same way, and checks
**real ∈ predicted**:

```bash
python3 scripts/check_traces.py ./rescflow /path/to/go1.26.5            # results/trace_check.txt
python3 scripts/check_traces.py ./rescflow /path/to/go1.26.5 --mutant-fifo   # negative control
```

| | Benchmarks (20) | Held-out (14) |
|---|---|---|
| Real runs (labelled + injected + extra) | 111 (23 + 88) | 100 (27 + 73) |
| **Real sequence = a predicted path** | **111** | **98** |
| Not covered (path predicted as incomplete) | 0 | 2 (H13: goroutine, flagged in advance) |
| **Mismatch** | **0** | **0** |
| Negative control (FIFO close order): mismatches caught | 11 runs / 5 programs | 17 runs / 6 programs |

**Predicted but never exercised** (no wrong prediction is involved; these are paths the inputs never take):
- loops: 4-iteration paths, because the analysis does not evaluate loop counters (08, H01, H02, H07);
- 04: the open of `path+"/missing"` succeeding;
- H03: 12 of 47 orderings, because the analysis does not know `i%2` alternates. This is the main
  imprecision, since it predicts every mix of "defer" and "explicit close" per iteration.

**Fault injection is made deterministic.** strace counts `when=N` per thread, and Go sometimes
issues the second `open` from another OS thread. An injected run is therefore accepted only if strace reports the
injection and all calls on the input file came from one thread. Otherwise it is retried (up to 25
times) or reported as not reproducible (0 here).

**Limits of the explorer:**
- Paths multiply when `main` chains several functions with branches: `testdata/fresh_check` (4 functions) gives 558 paths.
- The budget is 20,000 exits per program. Beyond it, output is marked `truncated`.
- Goroutines, unmodelled callees and dynamic calls mark a path `incomplete` instead of guessing.
