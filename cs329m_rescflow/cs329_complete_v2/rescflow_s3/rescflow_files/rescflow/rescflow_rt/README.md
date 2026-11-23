# rescflow_rt (step 4)

The runtime RescFlow's generated Rust links against. Extracted from the 18 LLM-written
translations in `Claude_translations/` (each carried its own copy) and generalized: a guard runs
any cleanup instead of the benchmarks' hard-wired trace printing. No dependencies; Unix only.

| Step-3 plan | Type | Go behaviour kept |
|---|---|---|
| `OWNED_GUARD` | `DeferClose::new(file)` / `DeferClose::with(file, cleanup)` | cleanup at function exit; arguments fixed at `defer`; LIFO by reverse drop order |
| `OPTION_GUARD` | `Option<DeferClose>` | defer registered on some paths only |
| `DEFER_STACK` | `DeferStack` | defer in a loop: all registrations run, last first |
| `CELL_CLOSURE_GUARD` | `Defer::new(closure)` + `RefCell<Option<File>>` | the closure reads the variable when it runs |
| `RESULT_SLOT_GUARD` | `Defer` + `close(file) -> io::Result<()>` | the close error is observed |
| `DYN_GUARD` | `Defer` over `Box<dyn ...>` | deferred interface call |
| B, transfer path | `DeferClose::into_inner()` | cancel the cleanup when ownership moves out |
| C, kept as Go behaves | `OsFile` | second close fails ("file already closed"), use after close fails |
| `+ CATCH_UNWIND` | `recover(body)` | guards run while a recovered panic unwinds; no panic message |
| unrecovered panic | `go_panic(msg)` | stderr `panic: msg`, exit status 2 |

`close` calls `close(2)` itself because `Drop for File` discards close errors. Like Go, `EINTR` counts as success.

## Checks

```bash
cargo test --offline                                         # 10 semantic tests, one Go behaviour each
python3 ../scripts/check_rt_ports.py /path/to/go1.26.5        # ported benchmarks vs Go: 28/28 identical
```

`examples/` ports benchmarks 01, 08, 10, 14 and 17 onto the crate, one per kind of plan.
`examples/common/bench.rs` holds the benchmarks' own instrumentation helpers (`recordOpen`,
`closeFile`, `useFile`). Those are program code, not runtime.
