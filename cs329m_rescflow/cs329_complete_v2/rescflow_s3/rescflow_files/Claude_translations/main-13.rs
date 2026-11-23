//! Case 09 `return_open` - OWNERSHIP_TRANSFER.
//! The open handle is returned: ownership moves to the caller. Go's
//! nil-or-*os.File return becomes `Option<File>`. The caller's explicit
//! closeFile consumes the file.
//! Translated from benchmarks/09_return_open/main.go.

use std::fs::File;
use std::io;
use std::os::fd::IntoRawFd;
use std::os::raw::c_int;
use std::sync::OnceLock;
fn scenario() -> Option<File> {
    let f = os_open(path()); // RF:acquire A
    let Ok(f) = record_open("A", f) else { return None };
    Some(f)
}

fn main() {
    parse_args();
    let f = scenario(); println!("returned"); if let Some(f) = f { use_file("A", &f); let _ = close_file("A", f); }
}

// ---- Go package-level `var path, mode string` --------------------------------
// Written once by main before scenario() runs, read-only afterwards: OnceLock.
static PATH: OnceLock<String> = OnceLock::new();
static MODE: OnceLock<String> = OnceLock::new();
fn path() -> &'static str { PATH.get().map(String::as_str).unwrap_or("") }
#[allow(dead_code)]
fn mode() -> &'static str { MODE.get().map(String::as_str).unwrap_or("") }

/// Go `panic(msg)` with no recover in the call stack: message on stderr, exit status 2.
/// (Rust's own `panic!` would exit with 101, which is a different observable result.)
fn go_panic(msg: &str) -> ! {
    eprintln!("panic: {msg}");
    std::process::exit(2)
}

fn parse_args() {
    let args: Vec<String> = std::env::args().collect();
    if args.len() < 2 { go_panic("pass input file path") }
    PATH.set(args[1].clone()).unwrap();
    if args.len() > 2 { MODE.set(args[2].clone()).unwrap(); }
}

// ---- os.Open / (*os.File).Close primitives -------------------------------------
fn os_open(p: &str) -> io::Result<File> { File::open(p) }

/// Go `recordOpen(id, f, err) bool`. Go returns (nil, err) or (f, nil); Rust's
/// `io::Result<File>` carries exactly one of the two, so the helper passes it through.
fn record_open(id: &str, r: io::Result<File>) -> io::Result<File> {
    println!("acquire {} {}", id, r.is_ok());
    r
}

/// close(2) that reports its result. `Drop for File` discards close errors, but Go's
/// Close returns them and the trace prints them. Like Go, EINTR counts as success.
fn sys_close(f: File) -> io::Result<()> {
    unsafe extern "C" { fn close(fd: c_int) -> c_int; }
    let fd = f.into_raw_fd(); // fd is no longer owned by `File`; we close it exactly once
    if unsafe { close(fd) } == 0 { return Ok(()); }
    let e = io::Error::last_os_error();
    if e.kind() == io::ErrorKind::Interrupted { Ok(()) } else { Err(e) }
}

// ---- Owned-handle model ---------------------------------------------------------
// The resource never outlives its owner and is never touched after Close, so a
// plain owned `File` is enough. Close consumes it: use-after-close cannot type-check.

/// Go `closeFile(id, f) error`.
fn close_file(id: &str, f: File) -> io::Result<()> {
    let err = sys_close(f);
    println!("close {} {}", id, err.is_ok());
    err
}

/// Go `useFile(id, f)` (probes with Stat).
#[allow(dead_code)]
fn use_file(id: &str, f: &File) {
    let r = f.metadata();
    println!("use {} {}", id, r.is_ok());
}
