## Review: U04_struct_owns_two, `a` (testdata/final_unseen/U04_struct_owns_two/main.go:8)

**Question:** On at least one path `a` is never closed. Is that intended?

**Source:**

```go
line 8: a, err := os.Open(path) // RF:acquire A
line 11: if !recordOpen("B", b, err) { closeFile("A", a); return }
line 13: defer p.Close() // RF:defer D1 A,B
line 14: useFile("A", p.a)
line 16: }
```

**Findings:**

- a path leaves the file open (may still be open when U04_struct_owns_two.scenario returns at line 16 (state Open|Closed)); is that intended?

| Option | Rust | What changes |
|---|---|---|
| No, it must be closed on every path | an owned `DeferClose` guard created right after the acquisition, replacing the path-dependent cleanup | the file is also closed on the path that missed it (one more close in the trace) |
| Yes, ownership moves somewhere else on that path | name the new owner; it then follows the transfer rules (return, global, out-parameter) | none, once the owner is stated |

**Until decided:** no Rust is generated for this function; the review report is the output.

## Review: U04_struct_owns_two, `b` (testdata/final_unseen/U04_struct_owns_two/main.go:10)

**Question:** On at least one path `b` is never closed. Is that intended?

**Source:**

```go
line 10: b, err := os.Open(path) // RF:acquire B
line 13: defer p.Close() // RF:defer D1 A,B
line 15: useFile("B", p.b)
line 16: }
```

**Findings:**

- a path leaves the file open (may still be open when U04_struct_owns_two.scenario returns at line 16 (state Open|Closed)); is that intended?

| Option | Rust | What changes |
|---|---|---|
| No, it must be closed on every path | an owned `DeferClose` guard created right after the acquisition, replacing the path-dependent cleanup | the file is also closed on the path that missed it (one more close in the trace) |
| Yes, ownership moves somewhere else on that path | name the new owner; it then follows the transfer rules (return, global, out-parameter) | none, once the owner is stated |

**Until decided:** no Rust is generated for this function; the review report is the output.

