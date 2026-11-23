# Step 3: translation rules (A / B / C)

`internal/analysis/rules.go` classifies every resource record that step 1 produces. Each decision
carries the **rule that fired**, the **facts** behind it, the chosen **plan** (a Rust template name), and the Rust
shape that plan stands for. B decisions also carry a **choice rule**; C decisions carry **review** reasons.
The rules read only the record. A fact that is not in the record is never assumed.

```bash
./rescflow -format text ./heldout/H12_path_dependent_ownership   # see "step 3: translation decisions"
python3 scripts/evaluate_rules.py ./rescflow                       # results/rules_eval.txt
```

## Categories and rule order (first match wins; C lists every reason)

| Order | Category | Fires when the record shows… | Output |
|---|---|---|---|
| 1 | **C** | the resource is in a goroutine; any `UNRESOLVED` note (unknown callee, dynamic call, several implementations, collection, recursion, unsafe, os.Exit); use after close; double close; "possible" use-after-close / double close; a path that leaves it open; Close of a failed open; an escape the analysis cannot follow | `REVIEW` + every reason, phrased as the question a human must answer |
| 2 | **B** | ownership leaves the function on one path (returned, out-parameter, global) **and** a defer in the acquiring function closes it on another | two plans + the choice rule: move the file out on the transferring path, use a guard elsewhere, return `Option<File>` |
| 3 | **A** | everything else: one plan, chosen per defer from the facts below | plan(s) + Rust shape |

## How an A plan is chosen

| Fact in the record | Plan | Rust shape |
|---|---|---|
| defer registered inside a loop | `DEFER_STACK` | `DeferStack`, popped LIFO at exit (a `Vec` would drop first-to-last) |
| defer registered on some branches only | `OPTION_GUARD` | `Option<DeferClose>`, set on the registering branch |
| the deferred closure's close error is stored into a named result | `RESULT_SLOT_GUARD` | explicit close in a guard that writes the result slot |
| closure reads a variable at exit **and** acts on different files on different paths | `CELL_CLOSURE_GUARD` | `RefCell<Option<File>>` read by the guard at exit |
| deferred interface method with one implementation | `DYN_GUARD` | `Box<dyn Closer>` in a guard |
| the defer is in a callee that received the file (nothing returned) | `CALLEE_OWNS` | the callee takes `File` by value |
| otherwise | `OWNED_GUARD` | `DeferClose` at function scope (hoisted out of inner blocks, as Go closes at function exit) |
| returned / global / out-parameter, with no local defer | `OWNERSHIP_RETURN` / `GLOBAL_OWNER` / `OUT_PARAM_TRANSFER` | ownership moves |
| explicit close, no defer | `EXPLICIT_CLOSE` | `close_file(f)` consumes the file |
| a defer runs on a recovered panic path | `+ CATCH_UNWIND` | `catch_unwind` around the body |

## Results (`results/rules_eval.txt`)

| Set | Agreement with expected category | Breakdown (expected → decided) |
|---|---|---|
| Benchmarks (20) | **20/20** | A→A 16, C→C 4 (10, 16, 18, 19) |
| Held-out (14) | **14/14** | A→A 11, B→B 1 (H12), C→C 2 (H13, H14) |

The plans match the design table too. Every loop gets `DEFER_STACK`; 14 and H04 get `CELL_CLOSURE_GUARD`;
H10 gets `RESULT_SLOT_GUARD`; H06 gets `CALLEE_OWNS`; 17 and H11 get `OWNED_GUARD + CATCH_UNWIND`; 20 gets `DYN_GUARD`;
H08 gets `OWNERSHIP_RETURN + OWNED_GUARD`. Every decision with its facts: `results/rules_decisions_*.txt`.

## How much this result shows

- **Held-out expected categories** were written in step 0, before any analysis or rule existed.
- **Benchmark expected categories** (`benchmarks/expected_categories.json`) were proposed from the
  design table before the rules were written, but by the same author, and they await team sign-off.
- The analysis was debugged on the held-out set (H03, H06, H10, H14), so 34/34 is a development result.
  **The real test is the sealed set** `testdata/final_unseen` (10 programs, expected 6 A / 1 B / 3 C). Neither the
  analysis nor the rules have been run on it.

## Known limits of the rules

- Plan selection reads the analysis's captured-variable descriptions ("read at exit" vs "fixed at registration")
  and escape strings. A change in that wording would need the same change here.
- H03 mixes a defer (even iterations) with explicit closes (odd iterations). The plan says `DEFER_STACK` only.
  The explicit close is implied by the code, not named in the plan.
- B covers one shape: transfer on one path plus a local defer on another. Other shapes with several valid
  plans (e.g. a file both stored and returned) currently fall into A or C.
- Case 16's deliberate double close is C by rule. If the team decides it should be B
  (`docs/label_review_round2.md`), that needs a rule distinguishing "double close the program
  relies on" from "accidental double close", which the record cannot tell apart today.

## Next

Step 4 extracts the Rust support library (`DeferClose`, `DeferStack`, `Defer`, `OsFile`, `close_file`) from the
LLM translations. Step 5 generates each plan from its template. Step 6 turns the `REVIEW` reasons into full review reports.
