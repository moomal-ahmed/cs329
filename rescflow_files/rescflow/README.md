# RescFlow: weeks 1-2 checkpoint package

Prepared 2026-10-26 for the October 27 checkpoint. Read `docs/checkpoint_report.pdf` first.

## Update 2026-11-23: step 1 analysis layer

`cmd/rescflow` is now the step-1 tool: go/ssa plus a memory-aware resource analysis (Go 1.26.x,
x/tools v0.50.0). See `docs/analysis_layer.md`. It finds 24/24 + 17/17 acquisition sites and
20/20 + 17/17 defers in the benchmark and held-out sets, and resolves every defer to the resource it acts on.
`testdata/limits/` shows each case it cannot parse or resolve, with the explanation it reports.
The pre-checkpoint prototype it replaces is in git history. The held-out programs are in `heldout/`.

## Update 2026-11-09: baselines executed

Both translators were run on all 20 programs. See `docs/translation_evaluation.md`.
Go reference: 23/23 inputs match labels (Go 1.27.0 and 1.26.5). Rustygo: 20/20 compile,
23/23 inputs match Go. Go2Rust as emitted: 0/20 compile (two bugs in its `os` stand-in).
With two stand-in-only patches: 19/23 inputs match, and a further hidden wrong-object close is
found in case 14. The status list below describes the original Oct 26 package.

## Evidence status

- Authored: 20 standalone Go programs, 23 input scenarios, 24 static acquisition sites,
  20 defer sites (19 resource-related, one recovery defer), line-level labels and expected traces.
- Executed: corpus consistency audit; lexical site inventory; seven small Python semantic-model
  checks (six mutant distinctions and one negative control); Python syntax validation.
- NOT executed: Go programs, Go SSA implementation, Go2Rust, Rustygo, or generated Rust.
  Missing compilers and denied direct downloads prevented those runs. Forty baseline rows
  are `blocked_environment`, not forty translation failures. No baseline success rate exists.
- Labels are author-derived and need independent Moomal/Rama review. Expected traces are
  not observed Go output. The corpus is synthetic, not 20 open-source codebases.

## Files

- `benchmarks/*/main.go`: one self-contained instrumented Go program per case.
- `benchmarks/labels.json`: static sites, paths, expected events, transfer classification.
- `cmd/rescflow/main.go`: uncompiled Go SSA site/value-origin analysis implementation.
- `scripts/lexical_inventory.py`: working Python lexical inventory; explicitly not SSA.
- `models/defer_model.py`: executable semantic model and intentionally wrong policies.
- `scripts/run_go.py`: compiles originals and compares observed output with draft oracles.
- `scripts/run_baselines.py`: preflight and initial baseline adapters; compiler paths unverified.
- `scripts/evaluate_ssa.py`: compares SSA output with draft site and association labels.
- `results/`: actual observations, including blocked runs. No fabricated SSA JSON is included.
- `docs/`: research register, report, checkpoint brief, review checklist, and design.

## Run the checks available without Go

From this directory:

```sh
python3 scripts/audit_corpus.py
python3 scripts/lexical_inventory.py
python3 models/defer_model.py
```

The lexical scanner ignores comments and strings. On this small authored corpus it finds
24/24 acquisition spellings and 20/20 defers, with zero extra sites. Its controls deliberately
show a false positive under `os` shadowing and a false negative for an aliased `os` import.
These are not semantic resource-analysis accuracy results.

## Enable the Go experiment on a machine with a toolchain

Use Go 1.27.x with the pinned x/tools v0.52.0 module, or deliberately choose and record a
compatible alternative. Those versions are pinned from retrieved documentation, NOT a
verified combination in this environment. `go.sum` is absent because dependencies could
not be downloaded. Preserve the generated `go.sum` and `go version` in the experiment.
Do not format the benchmark sources without refreshing and reviewing line labels.

```sh
go version
go mod tidy
go build -o rescflow ./cmd/rescflow
python3 scripts/run_go.py
./rescflow -benchmark-summaries -ssa-dir results/ssa ./benchmarks/... > results/ssa.json
python3 scripts/evaluate_ssa.py
```

The analyzer uses package/type information and SSA to identify known os constructors,
record Defer/RunDefers, and follow simple SSA origins through Extract, Phi, and casts.
It does not prove success guards, cleanup coverage, order, path feasibility, or ownership.
`-benchmark-summaries` explicitly enables the known `closeFile` helper summary for only
this module's benchmark packages. Without that flag, helper cleanups remain unresolved.
Closure cells, dynamic interfaces, borrowed helper parameters and heap fields are
intentionally unresolved. It inventories package functions and their anonymous functions;
method bodies are not enumerated independently in this initial implementation.

## Run the baselines honestly

1. Clone the exact repositories listed in `docs/research_register.json` on a networked Linux
   machine; record `git rev-parse HEAD`, dirty status, `go version`, `rustc -Vv`, and `cargo -V`.
   No commit hashes have been invented or pinned for baselines in this package.
2. Build Go2Rust with its documented `go build -o go2rust ./go`; build Rustygo's CLI
   with `go build -o rustygo ./cmd/rustygo` in its own checkout. Verify their current help.
3. Run originals first. Then invoke:

```sh
python3 scripts/run_baselines.py --go2rust /absolute/path/to/go2rust --rustygo /absolute/path/to/rustygo
```

Adapter limitations: Go2Rust stdout output is initially passed to rustc in single-file
mode. If it needs Cargo dependencies or a generated workspace, use the upstream project
build procedure and amend the adapter. Such errors must not be reported as language
translation failures. Rustygo emit is separate, but its build command regenerates and
compiles together; that failure is recorded as `combined_build_failed`. The adapter does
not yet separately compile the exact saved emit tree. The unexecuted code paths need a
smoke test before reporting comparative results. Preserve generated code and all logs.

## Trace and scope policy

The fixtures exercise real `os.Open` / `(*os.File).Close` / Stat calls. Small helper functions
print normalized acquisition, close-success and usability events. The helper itself returns
Close's actual error. Case 16 uses a second close for a deterministic failure, not a simulated
disk error. `useFile` probes with Stat, not a full read/write suite. Event labels are static
except loop instances. Failure paths other than the explicit case 04 are not fully covered;
case 14's B tag is meaningful only on the selected successful input. Trace logging adds
observable effects: report instrumented equivalence separately from uninstrumented behavior.

Do not silently repair source double-close or return-closed behavior when claiming translation
fidelity. No speed, GC reduction, resource-leak prevalence, or baseline-bug claims are supported.
