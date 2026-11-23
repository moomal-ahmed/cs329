//! Benchmark 01 `local`, plan OWNED_GUARD, ported from Claude_translations/main.rs onto rescflow_rt.
#[path = "common/bench.rs"]
mod bench;
use bench::*;
use rescflow_rt::DeferClose;

fn scenario() {
    let Ok(f) = record_open("A", os_open(path())) else { return }; // RF:acquire A
    let f = DeferClose::with(f, |f| { let _ = close_file("A", f); }); // RF:defer D1 A
    use_file("A", &f);
}
fn main() { parse_args(); scenario(); println!("returned"); }
