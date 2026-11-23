# Held-out programs (step 0)

14 new Go programs, written after the original 20 were translated and **before** any RescFlow
analysis code exists. Use them only for final evaluation. Do not tune RescFlow on them.

| ID | Pattern | Expected category | Failure injection |
|---|---|---|---|
| H01 loop_count | loop runs 0, 1 or 3 times | A | 2nd open fails |
| H02 two_per_iteration | two resources per loop iteration | A | 3rd open fails |
| H03 conditional_in_loop | defer on even iterations, explicit close on odd | A | |
| H04 closure_two_vars | closure captures two variables that may be swapped | A | 2nd open fails |
| H05 field_alias | defer argument read through a struct field | A | |
| H06 method_defer | defer inside a method that takes the file | A | |
| H07 early_return_in_loop | return from inside the loop after defers | A | |
| H08 factory_helper | resource opened by a helper that returns it | A | open fails |
| H09 nested_calls | caller and callee each own and defer a resource | A | 2nd open fails |
| H10 close_error_join | `if cerr := Close(); err == nil { err = cerr }` idiom | A | close fails, open fails |
| H11 panic_from_callee | panic raised by a callee, recovered by a defer | A | |
| H12 path_dependent_ownership | returned on one path, deferred-closed on the other | B | |
| H13 goroutine_defer | defer inside a goroutine (concurrency is out of scope) | C | |
| H14 dynamic_closer | deferred Close dispatches to one of two types; one leaks | C | |

`labels.json` uses the same fields as `benchmarks/labels.json`, plus:
- `expected_rescflow_category`
- `expected_plan`
- per-input `inject`, as `syscall:N:ERRNO`. The Nth call on the input file fails, via strace.

Regenerate the sources and labels with `python3 heldout/make_heldout.py`. Check them with
`python3 scripts/run_heldout_go.py <GOROOT>`: 27/27 inputs match on Go 1.26.5 and 1.27.0.
Expected traces were written first and then checked against Go. They still need review by Moomal and Rama.
