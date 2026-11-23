//! Case 16 `named_result` - EXPLICIT_STATE (deferred write to a named result).
//! Two Go behaviors are involved:
//!   (1) The deferred closure overwrites the named result after `return nil` has
//!       stored nil into it. A Rust Drop cannot change a value that is already
//!       being returned, so the named result becomes a `RefCell` slot. The guard
//!       writes into the slot. The slot is read only after the guard has run,
//!       which happens at the end of the inner block.
//!   (2) A is closed twice: explicitly, then by the defer. The source intends this
//!       so that the second close fails deterministically (labels.json). An owned
//!       `File` cannot be closed twice, so the handle is an `OsFile` that tracks
//!       its closed state.
//! Go `return X` statements after registration become `break 'body X`. They store
//! into the slot and then leave the block, so the defer still runs.
//! Translated from benchmarks/16_named_result/main.go.

use std::cell::RefCell;
use std::fs::File;
use std::io;
use std::os::fd::IntoRawFd;
use std::os::raw::c_int;
use std::sync::OnceLock;
fn scenario() -> io::Result<()> {
    let f = os_open(path()); // RF:acquire A
    let f = match record_open("A", f) { Ok(f) => OsFile::new(f), Err(err) => return Err(err) };
    let result: RefCell<io::Result<()>> = RefCell::new(Ok(())); // named result `result error`
    'body: {
        let _d1 = Defer::new(|| { *result.borrow_mut() = close_file("A", &f); }); // RF:defer D1 A
        let _ = close_file("A", &f);
        *result.borrow_mut() = Ok(()); // return nil  (store into the result slot ...)
        break 'body; //                  (... then leave: D1 runs and overwrites it)
    }
    result.into_inner()
}

fn main() {
    parse_args();
    let err = scenario(); println!("result-error {}", err.is_err());
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

// ---- Shared-handle model (Go's *os.File memory model) ---------------------------
// This program uses the handle after Close or closes it twice, so ownership alone
// cannot express it. `OsFile` models what a Go *os.File points to: one handle that
// Close invalidates in place. Every alias sees the change. A second Close returns
// ErrClosed and Stat on a closed handle fails. If an OsFile is dropped while still
// open, the std File's Drop closes the fd silently, like Go's GC finalizer.
struct OsFile { fd: RefCell<Option<File>> }
impl OsFile {
    fn new(f: File) -> Self { OsFile { fd: RefCell::new(Some(f)) } }
    fn close(&self) -> io::Result<()> {
        match self.fd.borrow_mut().take() {
            Some(f) => sys_close(f),
            None => Err(err_closed()),
        }
    }
    fn stat(&self) -> io::Result<std::fs::Metadata> {
        match self.fd.borrow().as_ref() {
            Some(f) => f.metadata(),
            None => Err(err_closed()),
        }
    }
}
/// Go's os.ErrClosed ("file already closed").
fn err_closed() -> io::Error { io::Error::other("file already closed") }

/// Go `closeFile(id, f) error`.
fn close_file(id: &str, f: &OsFile) -> io::Result<()> {
    let err = f.close();
    println!("close {} {}", id, err.is_ok());
    err
}

/// Go `useFile(id, f)` (probes with Stat).
#[allow(dead_code)]
fn use_file(id: &str, f: &OsFile) {
    let r = f.stat();
    println!("use {} {}", id, r.is_ok());
}

/// `defer func(){ ... }()`: runs the closure once, when the guard drops. Anything
/// the closure captures by `move` is evaluated at registration, like Go's deferred
/// arguments. Anything it captures by reference is read when it runs, like a Go closure.
struct Defer<F: FnOnce()>(Option<F>);
impl<F: FnOnce()> Defer<F> { fn new(f: F) -> Self { Defer(Some(f)) } }
impl<F: FnOnce()> Drop for Defer<F> {
    fn drop(&mut self) { if let Some(f) = self.0.take() { f() } }
}
