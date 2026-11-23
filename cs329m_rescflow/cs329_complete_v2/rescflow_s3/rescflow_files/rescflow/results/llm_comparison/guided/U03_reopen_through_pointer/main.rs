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
use rescflow_rt::{Defer, OsFile};
use std::cell::RefCell;

/// OUT_PARAM_TRANSFER: the new file `g` moves out through `fp`.
fn reopen(fp: &RefCell<OsFile>) -> bool {
    let g = File::open(path());
    if !record_open("B", &g) {
        return false;
    }
    let _ = close_os("A", &fp.borrow());
    *fp.borrow_mut() = OsFile::new(g.unwrap());
    true
}

fn close_os(id: &str, f: &OsFile) -> io::Result<()> {
    let r = f.close();
    println!("close {} {}", id, r.is_ok());
    r
}
fn use_os(id: &str, f: &OsFile) {
    let ok = f.stat().is_ok();
    println!("use {} {}", id, ok);
}

fn scenario() {
    let f = File::open(path());
    if !record_open("A", &f) {
        return;
    }
    // REVIEW: RescFlow category C for `f`: it reports a possible use after close (line 20) and a
    // double close (the deferred close at line 18 after reopen closed it). A human must decide
    // which side owns the file and which close is the real one. Until then Go's behaviour is
    // kept exactly with a run-time-checked OsFile (second close fails, use after close fails).
    let f = RefCell::new(OsFile::new(f.unwrap()));
    let name = RefCell::new("A");
    // defer func() { closeFile(name, f) }(): reads `name` and `f` when it runs.
    let _g = Defer::new(|| {
        let _ = close_os(&name.borrow(), &f.borrow());
    });
    if mode_is("reopen") && reopen(&f) {
        *name.borrow_mut() = "B";
    }
    use_os(&name.borrow(), &f.borrow());
}

fn main() {
    init_args();
    scenario();
    println!("returned");
}
