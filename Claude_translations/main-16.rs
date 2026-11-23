//! Case 20 `interface_dispatch` - LOCAL_CANDIDATE (dynamic dispatch).
//! `closer` becomes a trait and `tracked` becomes a struct that holds a copy of the
//! pointer. `defer c.Close()` evaluates the interface value c at registration, so
//! the boxed trait object moves into the guard. The caller still uses f directly
//! after building `tracked{f}`, so f and tracked.f are aliases. Tracked therefore
//! borrows a stack-owned `OsFile`, which can be closed through a shared reference.
//! The interface's Close error is discarded, as Go discards a deferred call's
//! results.
//! Translated from benchmarks/20_interface_dispatch/main.go.

use std::cell::RefCell;
use std::fs::File;
use std::io;
use std::os::fd::IntoRawFd;
use std::os::raw::c_int;
use std::sync::OnceLock;
fn scenario() {
    let f = os_open(path()); // RF:acquire A
    let Ok(f) = record_open("A", f) else { return };
    let f = OsFile::new(f);
    let c: Box<dyn Closer + '_> = Box::new(Tracked { f: &f });
    let _d1 = Defer::new(move || { let _ = c.close(); }); // RF:defer D1 A
    use_file("A", &f);
} // D1 runs: dynamic dispatch -> Tracked::close

trait Closer { fn close(&self) -> io::Result<()>; }
struct Tracked<'a> { f: &'a OsFile }
impl Closer for Tracked<'_> { fn close(&self) -> io::Result<()> { close_file("A", self.f) } }

fn main() {
    parse_args();
    scenario(); println!("returned");
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
