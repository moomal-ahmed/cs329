# RescFlow analysis of U09_callee_panics_owns_file

Program category: **A** (A = one supported plan, B = several plans + choice rule, C = needs human review)

## Resource `f` acquired at main.go:14 (in U09_callee_panics_owns_file.scenario)

- category: A
- plan: CALLEE_OWNS + CATCH_UNWIND
- Rust shape: fn callee(f: File) { let _g = DeferClose::new(id, f); ... } // ownership moves into the callee | std::panic::catch_unwind around the body; guards drop during unwinding
- fact: defer@8 in U09_callee_panics_owns_file.work: registration always; f: cell *f, read at exit
- fact: cleanup happens inside callee U09_callee_panics_owns_file.work
- fact: a deferred cleanup runs on a recovered panic path
