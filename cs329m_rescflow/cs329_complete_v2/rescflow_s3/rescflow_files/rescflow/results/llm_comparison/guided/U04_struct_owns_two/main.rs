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
use rescflow_rt::DeferOwn;

struct Pair {
    a: File,
    b: File,
}

impl Pair {
    /// Go's `(*pair).Close`: closes b, then a.
    fn close(self) {
        let _ = close_file("B", self.b);
        let _ = close_file("A", self.a);
    }
}

fn scenario() {
    let a = File::open(path());
    if !record_open("A", &a) {
        return;
    }
    let a = a.unwrap();
    let b = File::open(path());
    // REVIEW: RescFlow category C for `a` and `b`: it reports a path that may leave each file open
    // when scenario returns. A human must decide whether each file must be closed on every path
    // or whether ownership moves elsewhere. Go's behaviour is kept exactly: `a` is closed
    // explicitly when `b` fails to open, otherwise the pair owns both and closes them at exit.
    if !record_open("B", &b) {
        let _ = close_file("A", a);
        return;
    }
    let p = DeferOwn::new(Pair { a, b: b.unwrap() }, Pair::close); // defer p.Close()
    use_file("A", &p.a);
    use_file("B", &p.b);
}

fn main() {
    init_args();
    scenario();
    println!("returned");
}
