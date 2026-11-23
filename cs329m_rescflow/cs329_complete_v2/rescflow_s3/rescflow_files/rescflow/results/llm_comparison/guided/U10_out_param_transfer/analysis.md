# RescFlow analysis of U10_out_param_transfer

Program category: **B** (A = one supported plan, B = several plans + choice rule, C = needs human review)

## Resource `f` acquired at main.go:8 (in U10_out_param_transfer.scenario)

- category: B
- plan: OUT_PARAM_TRANSFER | OWNED_GUARD
- Rust shape: fn producer(out: &mut Option<File>) // ownership moves through the out-parameter + let f = DeferClose::new(id, file); // owned; dropped at function exit, in reverse declaration order
- choice rule: the defer is registered only on paths that keep the file, so: move the file out (OUT_PARAM_TRANSFER) on the transferring path, and create the guard on the others; the producer's result becomes Option<File>
- fact: escape: line 10: stored into memory reachable from input out
- fact: defer@11 in U10_out_param_transfer.scenario (always) acts on it
