# RescFlow analysis of U07_stored_closure_deferred

Program category: **A** (A = one supported plan, B = several plans + choice rule, C = needs human review)

## Resource `f` acquired at main.go:8 (in U07_stored_closure_deferred.scenario)

- category: A
- plan: OWNED_GUARD
- Rust shape: let f = DeferClose::new(id, file); // owned; dropped at function exit, in reverse declaration order
- fact: defer@12 in U07_stored_closure_deferred.scenario: registration always; f: cell t0(f), read at exit
