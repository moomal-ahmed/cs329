//! Benchmark 08 `loop`, plan DEFER_STACK, ported from Claude_translations/main-10.rs onto rescflow_rt.
#[path = "common/bench.rs"]
mod bench;
use bench::*;
use rescflow_rt::{DeferClose, DeferStack};

fn scenario() {
    let mut defers = DeferStack::new();
    for i in 0..3 {
        let id = i.to_string();
        let Ok(f) = record_open(&id, os_open(path())) else { return }; // RF:acquire A
        defers.push(DeferClose::with(f, move |f| { let _ = close_file(&id, f); })); // RF:defer D1 A
        println!("iteration {}", i);
    }
    println!("after-loop");
}
fn main() { parse_args(); scenario(); println!("returned"); }
