//! Benchmark 17 `panic_unwind`, plan OWNED_GUARD + CATCH_UNWIND, ported from main-8.rs.
#[path = "common/bench.rs"]
mod bench;
use bench::*;
use rescflow_rt::{recover, DeferClose};

fn scenario() {
    // RF:defer D0 none -- the recover boundary, registered first, runs last
    let r = recover(|| {
        let Ok(f) = record_open("A", os_open(path())) else { return }; // RF:acquire A
        let _d1 = DeferClose::with(f, |f| { let _ = close_file("A", f); }); // RF:defer D1 A
        println!("panic");
        std::panic::panic_any("test");
    });
    if r.is_err() { println!("recovered") }
}
fn main() { parse_args(); scenario(); println!("returned"); }
