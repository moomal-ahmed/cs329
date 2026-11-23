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

## Review: 16_named_result, `f` (benchmarks/16_named_result/main.go:8)

**Question:** `f` is closed twice. Which close is the real one?

**Source:**

```go
line 8: f, err := os.Open(path) // RF:acquire A
line 10: defer func() { result = closeFile("A", f) }() // RF:defer D1 A
line 11: closeFile("A", f)
```

**Findings:**

- closes the file twice (double close: already closed when released at line 10 (defer@10 at return@12)); which close is the real one?

| Option | Rust | What changes |
|---|---|---|
| The first (explicit) close is the real one | close explicitly with `rescflow_rt::close(f)` (consumes the file); drop the deferred close | the second close and the error it returns disappear; anything that reads that error (a named result, a printed status) changes |
| The deferred close is the real one | remove the explicit close; keep a `DeferClose` guard | the file closes at function exit instead of earlier; the second close's error disappears |
| Keep Go's behaviour exactly | `rescflow_rt::OsFile`; the second `close()` returns "file already closed" | none: output identical to Go |

**Until decided:** no Rust is generated for this function; the review report is the output.

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

## Review: 19_double_close, `f` (benchmarks/19_double_close/main.go:8)

**Question:** `f` is closed twice. Which close is the real one?

**Source:**

```go
line 8: f, err := os.Open(path) // RF:acquire A
line 10: defer closeFile("A", f) // RF:defer D1 A
line 11: closeFile("A", f)
```

**Findings:**

- closes the file twice (double close: already closed when released at line 10 (defer@10 at return@12)); which close is the real one?

| Option | Rust | What changes |
|---|---|---|
| The first (explicit) close is the real one | close explicitly with `rescflow_rt::close(f)` (consumes the file); drop the deferred close | the second close and the error it returns disappear; anything that reads that error (a named result, a printed status) changes |
| The deferred close is the real one | remove the explicit close; keep a `DeferClose` guard | the file closes at function exit instead of earlier; the second close's error disappears |
| Keep Go's behaviour exactly | `rescflow_rt::OsFile`; the second `close()` returns "file already closed" | none: output identical to Go |

**Until decided:** no Rust is generated for this function; the review report is the output.

