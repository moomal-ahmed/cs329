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

## Review: H14_dynamic_closer, `f` (heldout/H14_dynamic_closer/main.go:8)

**Question:** A fact needed to translate `f` is unavailable (dispatch-multi). On at least one path `f` is never closed. Is that intended?

**Source:**

```go
line 8: f, err := os.Open(path) // RF:acquire A
line 12: defer c.Close() // RF:defer D1 A?
line 13: useFile("A", f)
line 14: }
```

**Findings:**

- fact unavailable (dispatch-multi, line 12): interface method call with several possible implementations
- released by some but not all possible implementations at line 12 (H14_dynamic_closer.(fileCloser).Close, H14_dynamic_closer.(noopCloser).Close)
- a path leaves the file open (may still be open when H14_dynamic_closer.scenario returns at line 14 (state Open|Closed)); is that intended?
- why: the interface value is built from different concrete types on different paths, and the implementations do not have the same effect on the resource (one releases it, another does not)

| Option | Rust | What changes |
|---|---|---|
| Provide: decide which implementation is intended to own cleanup, or make the close explicit on every path | re-run RescFlow with that fact (a model row or a summary), then the normal rules apply | none expected; the missing fact decides the plan |
| No, it must be closed on every path | an owned `DeferClose` guard created right after the acquisition, replacing the path-dependent cleanup | the file is also closed on the path that missed it (one more close in the trace) |
| Yes, ownership moves somewhere else on that path | name the new owner; it then follows the transfer rules (return, global, out-parameter) | none, once the owner is stated |

**Until decided:** no Rust is generated for this function; the review report is the output.

