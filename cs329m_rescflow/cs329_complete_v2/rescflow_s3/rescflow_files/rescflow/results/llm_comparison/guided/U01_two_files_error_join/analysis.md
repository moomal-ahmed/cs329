# RescFlow analysis of U01_two_files_error_join

Program category: **A** (A = one supported plan, B = several plans + choice rule, C = needs human review)

## Resource `src` acquired at main.go:8 (in U01_two_files_error_join.scenario)

- category: A
- plan: OWNED_GUARD
- Rust shape: let f = DeferClose::new(id, file); // owned; dropped at function exit, in reverse declaration order
- fact: defer@10 in U01_two_files_error_join.scenario: registration always; src: value fixed at registration = {src@8}

## Resource `dst` acquired at main.go:11 (in U01_two_files_error_join.scenario)

- category: A
- plan: RESULT_SLOT_GUARD
- Rust shape: let result = RefCell::new(Ok(())); let _g = Defer::new(|| if let Err(e) = close_file(f) { /* write result */ }); // explicit close, error observed
- fact: defer@13 in U01_two_files_error_join.scenario: registration always; dst: cell t9(dst), read at exit; err: cell t0(err), read at exit
- fact: close error flows into named result "err"
