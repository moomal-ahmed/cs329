//! Benchmark 10 `return_closed` (category C, kept as Go behaves), OsFile, ported from main-18.rs.
#[path = "common/bench.rs"]
mod bench;
use bench::*;
use rescflow_rt::{Defer, OsFile};
use std::rc::Rc;

fn scenario() -> Option<Rc<OsFile>> {
    let Ok(f) = record_open("A", os_open(path())) else { return None }; // RF:acquire A
    let f = Rc::new(OsFile::new(f));
    let g = Rc::clone(&f);
    let _d1 = Defer::new(move || { let _ = close_file_shared("A", &g); }); // RF:defer D1 A
    Some(f) // evaluated first; then the guard closes the same handle
}
fn main() {
    parse_args();
    let f = scenario(); println!("returned"); if let Some(f) = f { use_file_shared("A", &f) }
}
