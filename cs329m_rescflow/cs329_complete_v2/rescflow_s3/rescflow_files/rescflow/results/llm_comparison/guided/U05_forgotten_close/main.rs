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
fn scenario() {
    let f = File::open(path());
    if !record_open("A", &f) {
        return;
    }
    let f = f.unwrap();
    if mode_is("skip") {
        println!("skip");
        // REVIEW: RescFlow category C for `f`: on this path Go never closes the file (it stays
        // open until the process exits). A human must decide whether it must be closed on every
        // path (DeferClose guard, one more close) or whether ownership moves elsewhere. Go's
        // behaviour is kept exactly: the descriptor is deliberately leaked, not closed.
        std::mem::forget(f);
        return;
    }
    use_file("A", &f);
    let _ = close_file("A", f);
}

fn main() {
    init_args();
    scenario();
    println!("returned");
}
