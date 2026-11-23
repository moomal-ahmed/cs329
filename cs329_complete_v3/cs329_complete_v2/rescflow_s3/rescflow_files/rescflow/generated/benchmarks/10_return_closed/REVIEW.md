## Review: 10_return_closed, `f` (benchmarks/10_return_closed/main.go:8)

**Question:** `f` is used after it is closed. Which side owns the file?

**Source:**

```go
line 8: f, err := os.Open(path) // RF:acquire A
line 10: defer closeFile("A", f) // RF:defer D1 A
line 20: f := scenario(); fmt.Println("returned"); if f != nil { useFile("A", f) }
```

**Findings:**

- uses the file after it is closed (use after close at line 20 (call 10_return_closed.useFile)); Rust ownership rejects this. Which side should own the file?

| Option | Rust | What changes |
|---|---|---|
| The code that closes it owns it; the later use is a bug | the closer takes `File` by value (`DeferClose` there); the later use is deleted | the later use and its output disappear |
| The later user owns it; the closer only borrows | the closer takes `&File` and does not close; the owner keeps a `DeferClose` and closes after its last use | the file closes later; the later use now succeeds |
| Keep Go's behaviour exactly | `rescflow_rt::OsFile` (shared, run-time-checked handle) | none: the use after close still fails at run time, as in Go |

**Until decided:** no Rust is generated for this function; the review report is the output.

