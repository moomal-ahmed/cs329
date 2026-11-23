# RescFlow analysis of U08_kept_in_registry

Program category: **C** (A = one supported plan, B = several plans + choice rule, C = needs human review)

## Resource `f` acquired at main.go:8 (in U08_kept_in_registry.scenario)

- category: C
- plan: REVIEW
- Rust shape: no translation emitted: review report
- fact: fact unavailable (collection, line 10): resource stored in a slice, array element, map or channel
- fact: escapes where ownership cannot be followed: line 10: stored in a map or sent on a channel

# Review report

## Review: U08_kept_in_registry, `f` (testdata/final_unseen/U08_kept_in_registry/main.go:8)

**Question:** A fact needed to translate `f` is unavailable (collection).

**Source:**

```go
line 8: f, err := os.Open(path) // RF:acquire A
line 11: defer closeFile("A", f) // RF:defer D1 A
line 12: useFile("A", f)
```

**Findings:**

- fact unavailable (collection, line 10): resource stored in a slice, array element, map or channel
- escapes where ownership cannot be followed: line 10: stored in a map or sent on a channel
- why: the analysis does not distinguish elements of collections, so every element is one abstract cell and updates are never exact

| Option | Rust | What changes |
|---|---|---|
| Provide: state whether the collection owns its resources (Vec<File> dropped together) and in which order they must be closed | re-run RescFlow with that fact (a model row or a summary), then the normal rules apply | none expected; the missing fact decides the plan |

**Until decided:** no Rust is generated for this function; the review report is the output.

