# Independent label review

These are draft author labels. No teammate review has been performed.

For every row check acquisition success branch; saved receiver vs mutable cell; function boundary; all registered defers; selected-input order; and escape policy.

| Case | Pattern | Acquisition lines | Defer lines | Expected cleanup | Handle leaves scenario? | Moomal review | Rama review |
|---|---|---|---|---|---|---|---|
| 01 | local | 8 | 10 | A | none | pending | pending |
| 02 | early_before | 9 | 11 | A | none | pending | pending |
| 03 | early_after | 8 | 10 | A | none | pending | pending |
| 04 | failed_acquire | 8 | 10 | none (acquisition fails) | none | pending | pending |
| 05 | two_lifo | 8, 11 | 10, 13 | B,A | none | pending | pending |
| 06 | nested_block | 9 | 11 | A at function exit, not block exit | none | pending | pending |
| 07 | conditional | 8 | 11 | A deferred only in defer mode; otherwise explicit | none | pending | pending |
| 08 | loop | 10 | 12 | 2,1,0; one static acquisition site, three instances | none | pending | pending |
| 09 | return_open | 8 |  | caller closes A | return | pending | pending |
| 10 | return_closed | 8 | 10 | A before caller receives handle | return | pending | pending |
| 11 | global_escape | 8 |  | caller closes A | global | pending | pending |
| 12 | alias | 8 | 11 | A | none | pending | pending |
| 13 | eager_argument | 8, 11 | 10 | explicit B, then deferred A | none | pending | pending |
| 14 | late_closure | 8, 11 | 10 | explicit A, then deferred B | none | pending | pending |
| 15 | closure_parameter | 8, 11 | 10 | explicit B, then deferred A | none | pending | pending |
| 16 | named_result | 8 | 10 | explicit A success, deferred A error | none | pending | pending |
| 17 | panic_unwind | 9 | 8, 11 | A then recovery | none | pending | pending |
| 18 | helper_boundary | 8 | 15 | A at helper exit | borrow-to-helper | pending | pending |
| 19 | double_close | 8 | 10 | explicit A then deferred A error | none | pending | pending |
| 20 | interface_dispatch | 8 | 11 | A through interface | none | pending | pending |
