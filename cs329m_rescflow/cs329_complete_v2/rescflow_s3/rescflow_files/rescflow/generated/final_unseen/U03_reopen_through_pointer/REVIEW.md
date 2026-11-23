## Review: U03_reopen_through_pointer, `f` (testdata/final_unseen/U03_reopen_through_pointer/main.go:15)

**Question:** `f` is used after it is closed. Which side owns the file? `f` is closed twice. Which close is the real one?

**Source:**

```go
line 15: f, err := os.Open(path) // RF:acquire A
line 18: defer func() { closeFile(name, f) }() // RF:defer D1 A,B
line 19: if mode == "reopen" && reopen(&f) { name = "B" }
line 20: useFile(name, f)
```

**Findings:**

- uses the file after it is closed (use after close at line 20 (call U03_reopen_through_pointer.useFile)); Rust ownership rejects this. Which side should own the file?
- closes the file twice (double close: already closed when released at line 18 (defer@18 at return@21)); which close is the real one?

| Option | Rust | What changes |
|---|---|---|
| The code that closes it owns it; the later use is a bug | the closer takes `File` by value (`DeferClose` there); the later use is deleted | the later use and its output disappear |
| The later user owns it; the closer only borrows | the closer takes `&File` and does not close; the owner keeps a `DeferClose` and closes after its last use | the file closes later; the later use now succeeds |
| Keep Go's behaviour exactly | `rescflow_rt::OsFile` (shared, run-time-checked handle) | none: the use after close still fails at run time, as in Go |
| The first (explicit) close is the real one | close explicitly with `rescflow_rt::close(f)` (consumes the file); drop the deferred close | the second close and the error it returns disappear; anything that reads that error (a named result, a printed status) changes |
| The deferred close is the real one | remove the explicit close; keep a `DeferClose` guard | the file closes at function exit instead of earlier; the second close's error disappears |
| Keep Go's behaviour exactly | `rescflow_rt::OsFile`; the second `close()` returns "file already closed" | none: output identical to Go |

**Until decided:** no Rust is generated for this function; the review report is the output.

