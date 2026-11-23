## Review: H13_goroutine_defer, `f` (heldout/H13_goroutine_defer/main.go:12)

**Question:** A fact needed to translate `f` is unavailable (goroutine).

**Source:**

```go
line 12: f, err := os.Open(path) // RF:acquire A
line 14: defer closeFile("A", f) // RF:defer D1 A
line 15: useFile("A", f)
```

**Findings:**

- acquired inside a goroutine: cleanup order relative to the caller is unknown
- fact unavailable (goroutine, line 12): resource acquired, used or released inside a goroutine (`go` statement)
- why: the analysis models a single sequential call stack. A defer inside a goroutine runs when that goroutine's function returns; that moment is ordered with the parent only through synchronization (WaitGroup, channels, mutexes) whose semantics RescFlow does not interpret

| Option | Rust | What changes |
|---|---|---|
| Provide: state the synchronization contract (e.g. `wg.Wait()` joins the goroutine before the caller continues) and whether the resource may move to another thread | re-run RescFlow with that fact (a model row or a summary), then the normal rules apply | none expected; the missing fact decides the plan |

**Until decided:** no Rust is generated for this function; the review report is the output.

