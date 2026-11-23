// Translated from main.go; resource plans follow analysis.md.
#![allow(dead_code)]
use std::ffi::OsString;
use std::fs::File;
use std::io;
use std::sync::OnceLock;

static PATH: OnceLock<OsString> = OnceLock::new();
static MODE: OnceLock<OsString> = OnceLock::new();

fn path() -> &'static OsString {
    PATH.get().expect("path set in main")
}
fn mode_is(s: &str) -> bool {
    MODE.get().map_or(false, |m| m.as_os_str() == s)
}

/// Go's `os.Args` handling at the top of main: panics (exit 2) without a path.
fn init_args() {
    let args: Vec<OsString> = std::env::args_os().collect();
    if args.len() < 2 {
        rescflow_rt::go_panic("pass input file path");
    }
    let _ = PATH.set(args[1].clone());
    if args.len() > 2 {
        let _ = MODE.set(args[2].clone());
    }
}

fn record_open(id: &str, r: &io::Result<File>) -> bool {
    let ok = r.is_ok();
    println!("acquire {} {}", id, ok);
    ok
}
fn close_file(id: &str, f: File) -> io::Result<()> {
    let r = rescflow_rt::close(f);
    println!("close {} {}", id, r.is_ok());
    r
}
fn use_file(id: &str, f: &File) {
    let ok = f.metadata().is_ok();
    println!("use {} {}", id, ok);
}
use rescflow_rt::DeferClose;

fn scenario() {
    let ids = ["0", "1", "2"];
    // OWNED_GUARD: the defer in the loop is registered at most once (break follows it),
    // so a single function-scope guard holds it until scenario returns.
    let mut _guard = None;
    for i in 0..3 {
        let f = File::open(path());
        if !record_open(ids[i], &f) {
            continue;
        }
        let id = ids[i];
        let g = DeferClose::with(f.unwrap(), move |f| {
            let _ = close_file(id, f);
        });
        use_file(ids[i], &g);
        _guard = Some(g);
        break;
    }
    println!("after-loop");
}

fn main() {
    init_args();
    scenario();
    println!("returned");
}
