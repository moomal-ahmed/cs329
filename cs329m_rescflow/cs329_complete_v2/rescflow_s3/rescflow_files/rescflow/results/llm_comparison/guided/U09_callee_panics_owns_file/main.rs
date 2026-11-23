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
use rescflow_rt::{go, recover, DeferClose};

/// CALLEE_OWNS: ownership of the file moves into `work`, whose guard closes it, also while a
/// panic unwinds.
fn work(f: File) {
    let f = DeferClose::with(f, |f| {
        let _ = close_file("A", f);
    });
    use_file("A", &f);
    if mode_is("panic") {
        go::panic("boom".to_string());
    }
}

fn scenario() {
    // CATCH_UNWIND: defer func() { if recover() != nil { fmt.Println("recovered") } }()
    let r = recover(|| {
        let f = File::open(path());
        if !record_open("A", &f) {
            return;
        }
        work(f.unwrap());
        println!("after-work");
    });
    if r.is_err() {
        println!("recovered");
    }
}

fn main() {
    init_args();
    scenario();
    println!("returned");
}
