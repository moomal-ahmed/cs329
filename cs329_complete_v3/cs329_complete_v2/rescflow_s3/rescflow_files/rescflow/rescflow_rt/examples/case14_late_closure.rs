//! Benchmark 14 `late_closure`, plan CELL_CLOSURE_GUARD, ported from Claude_translations/main-14.rs.
#[path = "common/bench.rs"]
mod bench;
use bench::*;
use rescflow_rt::Defer;
use std::cell::RefCell;

fn scenario() {
    let Ok(f) = record_open("A", os_open(path())) else { return }; // RF:acquire A
    let f = RefCell::new(Some(f));
    let _d1 = Defer::new(|| { // RF:defer D1 B  -- reads f when it runs
        if let Some(cur) = f.borrow_mut().take() { let _ = close_file("B", cur); }
    });
    let Ok(g) = record_open("B", os_open(path())) else { return }; // RF:acquire B
    let old = f.replace(Some(g)).expect("f is set");
    let _ = close_file("A", old);
    use_file("B", f.borrow().as_ref().expect("f is set"));
}
fn main() { parse_args(); scenario(); println!("returned"); }
