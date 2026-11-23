# RescFlow analysis of U05_forgotten_close

Program category: **C** (A = one supported plan, B = several plans + choice rule, C = needs human review)

## Resource `f` acquired at main.go:8 (in U05_forgotten_close.scenario)

- category: C
- plan: REVIEW
- Rust shape: no translation emitted: review report
- fact: a path leaves the file open (may still be open when U05_forgotten_close.scenario returns at line 10 (state Open)); is that intended?

# Review report

## Review: U05_forgotten_close, `f` (testdata/final_unseen/U05_forgotten_close/main.go:8)

**Question:** On at least one path `f` is never closed. Is that intended?

**Source:**

```go
line 8: f, err := os.Open(path) // RF:acquire A
line 10: if mode == "skip" { fmt.Println("skip"); return }
line 11: useFile("A", f)
line 12: closeFile("A", f)
```

**Findings:**

- a path leaves the file open (may still be open when U05_forgotten_close.scenario returns at line 10 (state Open)); is that intended?

| Option | Rust | What changes |
|---|---|---|
| No, it must be closed on every path | an owned `DeferClose` guard created right after the acquisition, replacing the path-dependent cleanup | the file is also closed on the path that missed it (one more close in the trace) |
| Yes, ownership moves somewhere else on that path | name the new owner; it then follows the transfer rules (return, global, out-parameter) | none, once the owner is stated |

**Until decided:** no Rust is generated for this function; the review report is the output.

