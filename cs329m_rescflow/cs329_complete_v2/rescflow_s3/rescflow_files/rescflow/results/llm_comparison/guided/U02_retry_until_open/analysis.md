# RescFlow analysis of U02_retry_until_open

Program category: **A** (A = one supported plan, B = several plans + choice rule, C = needs human review)

## Resource `f` acquired at main.go:10 (in U02_retry_until_open.scenario)

- category: A
- plan: OWNED_GUARD
- Rust shape: let f = DeferClose::new(id, file); // owned; dropped at function exit, in reverse declaration order
- fact: defer@12 in U02_retry_until_open.scenario: registration always; f: value fixed at registration = {f@10}
