# RescFlow analysis of U06_bufio_reader

Program category: **C** (A = one supported plan, B = several plans + choice rule, C = needs human review)

## Resource `f` acquired at main.go:8 (in U06_bufio_reader.scenario)

- category: C
- plan: REVIEW
- Rust shape: no translation emitted: review report
- fact: fact unavailable (unknown-callee, line 11): resource passed to a function without an analyzable body
- fact: escapes where ownership cannot be followed: line 11: passed to bufio.NewReader (no analyzable body (outside the analyzed packages))

# Review report

## Review: U06_bufio_reader, `f` (testdata/final_unseen/U06_bufio_reader/main.go:8)

**Question:** A fact needed to translate `f` is unavailable (unknown-callee).

**Source:**

```go
line 8: f, err := os.Open(path) // RF:acquire A
line 10: defer closeFile("A", f) // RF:defer D1 A
```

**Findings:**

- fact unavailable (unknown-callee, line 11): resource passed to a function without an analyzable body
- escapes where ownership cannot be followed: line 11: passed to bufio.NewReader (no analyzable body (outside the analyzed packages))
- why: the callee lives outside the analyzed packages (standard library without a model, assembly, cgo) so no summary exists

| Option | Rust | What changes |
|---|---|---|
| Provide: add a summary for the callee to the API model (does it close, use, store or return the resource?) | re-run RescFlow with that fact (a model row or a summary), then the normal rules apply | none expected; the missing fact decides the plan |

**Until decided:** no Rust is generated for this function; the review report is the output.

