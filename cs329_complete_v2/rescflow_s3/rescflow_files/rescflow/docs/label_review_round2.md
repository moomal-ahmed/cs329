# Label review, round 2 (34 programs)

Prepared 2026-11-23 by Claude as an independent pre-review. **This does not replace the
teammate review.** Moomal and Rama still each need to read every program and sign off below.

## 1. Automated consistency check: all 34 pass

`python3 scripts/review_labels.py` (output in `results/label_review.json`) checks, for
`benchmarks/labels.json` (20 programs) and `heldout/labels.json` (14):

| Check | Result |
|---|---|
| Every `// RF:` comment has a label site with the same line, kind, label and source text, and vice versa | 78/78 sites |
| Every expected output and exit code equals what real Go printed (Go 1.26.5; benchmarks also 1.27.0) | 50/50 inputs, including 7 injected failures |
| Every `close X` / `use X` in an expected output names an acquired label or a loop id | all |
| Every held-out program carries an expected category and plan | 14/14 |
| Labelled defer → resource links agree with the analysis | 36/37 exact. Case 14 is path-dependent (see below) |

So the labels are *mechanically* right. Mechanical checks cannot tell whether a label says what
we *mean*, so the points below are for human judgment.

## 2. Points for the two of you to decide

| Program | Observation | Recommendation |
|---|---|---|
| 04 failed_acquire | The open is `path + "/missing"`, so it fails on every input. The defer on line 10 is never reached in any run | Keep. Optionally add a note that line 10 is unreachable in tests |
| 14 late_closure | Label `D1 B` holds only when B opens. If B's open fails, the closure closes **A** while printing "B" (confirmed by the injected run) | Change the defer label to `D1 B (A if B fails)`, or keep `D1 B` and leave the note as is. **Decide** |
| 16 named_result | The double close is deliberate: the named result depends on the second close failing | Expected category set to **C** (double close) in `expected_categories.json`. The LLM translation kept it (B-like, via `OsFile`). **Decide C or B** |
| 18 helper_boundary | `escape: borrow-to-helper`, but the helper's defer *closes* the file, and the caller then uses it after close. This is not a borrow | Change `escape` to `helper closes it (ownership moves), caller uses after close` |
| 10 / 19 | Deliberate use-after-close / double close, kept as written per the original policy | Keep. Expected category **C** |
| 08 / H01–H03 / H07 | Loop sites are labelled `A`, while outputs use iteration ids (`0`, `1`, `2`, `0a`…) | Keep. The review script accepts loop ids |
| H14 dynamic_closer | Defer label `D1 A?`: the `?` means "only with `fileCloser`" | Spell it out: `D1 A (fileCloser only; noopCloser leaks A)` |
| Benchmarks | Had no expected categories (only `heldout/` did), so step 3 had nothing to score against | Added `benchmarks/expected_categories.json`. It was **proposed** from the design table, before the rules were written. **Review each row** |

## 3. Sign-off

Each reviewer reads every program's `main.go` next to its label entry and checks: acquisition lines;
which defer closes what, on which path; expected output for every input, including the injected ones;
escape; and the expected category.

| Set | Programs | Moomal | Rama |
|---|---|---|---|
| benchmarks/ | 01–20 | [ ] | [ ] |
| benchmarks/expected_categories.json | 20 rows | [ ] | [ ] |
| heldout/ | H01–H14 | [ ] | [ ] |
| Section 2 decisions | 8 rows | [ ] | [ ] |

When a label changes, run `python3 scripts/run_go.py`, `python3 scripts/run_heldout_go.py <GOROOT>` and
`python3 scripts/review_labels.py` again. All three must stay clean.
