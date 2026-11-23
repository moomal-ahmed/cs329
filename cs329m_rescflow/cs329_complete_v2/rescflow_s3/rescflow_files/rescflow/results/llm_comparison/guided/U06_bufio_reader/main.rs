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
use std::io::{BufRead, BufReader};

fn scenario() {
    let f = File::open(path());
    if !record_open("A", &f) {
        return;
    }
    // defer closeFile("A", f)
    let f = DeferClose::with(f.unwrap(), |f| {
        let _ = close_file("A", f);
    });
    // REVIEW: RescFlow category C for `f`: it has no summary for bufio.NewReader (does it close,
    // use, store or return the file?). A human must confirm the reader only borrows the file.
    // Kept as Go behaves: the reader borrows `&File` and the deferred guard owns and closes it.
    let mut r = BufReader::with_capacity(4096, &*f);
    let mut line = Vec::new();
    let _ = r.read_until(b'\n', &mut line);
    println!("read {}", !line.is_empty());
}

fn main() {
    init_args();
    scenario();
    println!("returned");
}
