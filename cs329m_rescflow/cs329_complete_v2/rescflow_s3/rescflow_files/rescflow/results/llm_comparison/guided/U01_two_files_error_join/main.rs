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
use rescflow_rt::{Defer, DeferClose};
use std::cell::RefCell;

fn scenario() -> io::Result<()> {
    let src = File::open(path());
    if !record_open("A", &src) {
        return Err(src.unwrap_err());
    }
    // OWNED_GUARD: defer closeFile("A", src)
    let src = DeferClose::with(src.unwrap(), |f| {
        let _ = close_file("A", f);
    });
    let dst = File::open(path());
    if !record_open("B", &dst) {
        return Err(dst.unwrap_err());
    }
    // RESULT_SLOT_GUARD: the deferred close of dst writes its error into the named result.
    let result: RefCell<io::Result<()>> = RefCell::new(Ok(()));
    let dst = RefCell::new(Some(dst.unwrap()));
    {
        let _g = Defer::new(|| {
            if let Some(f) = dst.borrow_mut().take() {
                let cerr = close_file("B", f);
                let mut r = result.borrow_mut();
                if r.is_ok() {
                    *r = cerr;
                }
            }
        });
        use_file("A", &src);
        use_file("B", dst.borrow().as_ref().expect("dst open"));
        // `return nil`: result stays Ok; _g runs now (D2), then `src` drops (D1).
    }
    result.into_inner()
}

fn main() {
    init_args();
    let err = scenario();
    println!("result-error {}", err.is_err());
}
