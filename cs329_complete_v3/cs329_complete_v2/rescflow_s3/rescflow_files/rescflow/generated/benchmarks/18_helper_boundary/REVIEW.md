## Review: 18_helper_boundary, `f` (benchmarks/18_helper_boundary/main.go:8)

**Question:** `f` is used after it is closed. Which side owns the file?

**Source:**

```go
line 8: f, err := os.Open(path) // RF:acquire A
line 10: helper(f)
line 11: useFile("A", f)
line 15: defer closeFile("A", f) // RF:defer D1 A
```

**Findings:**

- uses the file after it is closed (use after close at line 11 (call 18_helper_boundary.useFile)); Rust ownership rejects this. Which side should own the file?

| Option | Rust | What changes |
|---|---|---|
| The code that closes it owns it; the later use is a bug | the closer takes `File` by value (`DeferClose` there); the later use is deleted | the later use and its output disappear |
| The later user owns it; the closer only borrows | the closer takes `&File` and does not close; the owner keeps a `DeferClose` and closes after its last use | the file closes later; the later use now succeeds |
| Keep Go's behaviour exactly | `rescflow_rt::OsFile` (shared, run-time-checked handle) | none: the use after close still fails at run time, as in Go |

**Until decided:** no Rust is generated for this function; the review report is the output.

