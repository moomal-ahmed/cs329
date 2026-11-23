# Final evaluation (sealed set) and LLM comparison

Unsealed 2026-11-23 23:02 UTC (`results/final_unseal_time.txt`); the seal's checksums verified (13/13) just before.
Script: `python3 scripts/final_evaluation.py <rescflow> <go1.26.5> <go2rust> <rustygo> --llm-plain DIR --llm-guided DIR`.
Results: `results/final_evaluation.{txt,json}` (first run without the LLMs: `final_evaluation_first_run.*`, identical numbers).

## Setup

10 sealed programs (U01–U10), each run under 59 comparisons in total. Per program: every labelled input; for each mode,
"the k-th open fails" for every k and "the first close fails" (strace injection); a missing file; no arguments. Compared
with the Go original: stdout, exit code, first stderr line, OS open/close order.

| Tool | What it is |
|---|---|
| RescFlow | this project: analysis → rules → deterministic generation (A/B) or review report (C); no LLM |
| Go2Rust `3e81880e` | as emitted, and with the two stand-in patches used earlier (reported separately) |
| Rustygo `cf2f583d` | compiles Go's SSA to Rust with its own GC and runtime |
| LLM, plain prompt | a fresh agent given only `main.go` (prompt below) |
| LLM + RescFlow analysis | a fresh agent given `main.go` + RescFlow's analysis + the `rescflow_rt` API |

Both LLM conditions used the same model, ran once, in parallel, as independent agents with the same tools (rustc/cargo,
and Go to run the originals), and were told to read nothing else on the machine.

## Results

| Tool | Compiled | Programs identical to Go | Runs identical to Go | Expected-C programs surfaced for review |
|---|---|---|---|---|
| RescFlow | 5/10 (5 sent to review) | 5/10 | **28/28** | **3/3** |
| Go2Rust, as emitted | 0/10 | 0/10 | — | 0/3 |
| Go2Rust, 2 patches | 8/10 | 0/10 | 31/46 | 0/3 |
| Rustygo | 10/10 | 10/10 | 59/59 | 0/3 |
| LLM, plain prompt | 10/10 | 10/10 | 59/59 | 0/3 |
| LLM + RescFlow analysis | 10/10 | 10/10 | 59/59 | **3/3** (+2 false, see below) |

**RescFlow analysis:** acquisition sites 13/13, defer sites 11/11, categories agree **8/10**. U03 and U04 (expected A)
were classified C, so RescFlow emitted review reports instead of Rust; both errors are conservative:

- **U03** — `reopen(&f)` closes the old file and stores a new one through the pointer. Function summaries record "closes
  what the pointer holds" but not "stores a fresh file into it", so the caller still believes `f` holds the closed file and
  reports a use-after-close and a double close that do not happen. Fix: summaries need a "writes fresh resource to input k"
  effect.
- **U04** — one deferred method closes both files of a struct. Because one call releases two objects, each release is
  treated as "may", so the analysis reports a possible leak. Fix: a release of every resource reachable from an input should
  be definite for each of them.

Everything RescFlow did generate (U01 `OWNED_GUARD + RESULT_SLOT_GUARD`, U02 `OWNED_GUARD` — the analysis saw that the
`break` makes the loop's defer register at most once — U07, U09 `CALLEE_OWNS + CATCH_UNWIND`, U10 B
`OUT_PARAM_TRANSFER | OWNED_GUARD`) is identical to Go on every run, and all three true C programs (U05 leak, U06 unmodelled
`bufio`, U08 map) were surfaced with review reports.

## LLM: plain prompt vs RescFlow analysis

On behaviour, the analysis made no difference: both conditions produced 10/10 programs identical to Go on 59/59 runs.
The differences are in the code:

| | Plain prompt | + RescFlow analysis |
|---|---|---|
| Lines of Rust (10 programs) | 1342 | **765** (−43%) |
| Files with `unsafe` | **10/10** (each re-implements a raw `close`) | **0/10** (uses `rescflow_rt`) |
| Shared / run-time-checked handles | 0 | 1 (`OsFile` in U03, marked REVIEW) |
| Ownership conflicts surfaced (`// REVIEW:`) | 0 of 3 true C | **3 of 3** true C (+ U03, U04 inherited from RescFlow's false C) |
| U09's Go `panic` | rewritten as a returned error (same observable behaviour, different structure) | kept as a panic, stopped with `recover` (`catch_unwind`) |

So the hypothesis "analysis-guided LLM translation is better than plain-prompt translation" is **supported for code
structure and for surfacing intent conflicts, not for behavioural equivalence**, where a capable LLM agent that can
compile and run the Go original already reaches 59/59 on these small programs. The guided agent also inherited RescFlow's
two false C flags: an analysis error propagates into the LLM's output as a spurious review comment (harmless here).

## Prompts (verbatim summary)

*Plain:* translate each `main.go` to a single-file, std-only, edition-2021 `main.rs` that behaves exactly like Go — same
stdout, exit code, first stderr line and OS open/close behaviour, also when opens or closes fail, the file is missing, or
no arguments are given; idiomatic, using ownership where possible; may compile and run Go to compare; work only in the
given directory.

*Guided:* the same, plus: `analysis.md` gives each resource's category, plan, Rust shape and facts (and a review report
for C); use `rescflow_rt` (API summary and `src/lib.rs` provided); follow each plan; for a C resource preserve Go's
behaviour (e.g. `OsFile`) and mark the open question with `// REVIEW:`.

Both translation sets, the analyses given to the guided agent, and its `Cargo.toml` files are in
`results/llm_comparison/{plain,guided}/`.

## Threats to validity

- 10 small synthetic programs; one run per LLM condition (no variance estimate); one model.
- The LLM agents could compile and run the Go originals — they are agentic translators, not single-shot prompts.
- RescFlow's author wrote the sealed programs and their expected categories (after building the analysis, before the rules).
- Go2Rust and Rustygo at one pinned commit each.
- No runtime or memory measurements (proposal experiment 3 remains open).
