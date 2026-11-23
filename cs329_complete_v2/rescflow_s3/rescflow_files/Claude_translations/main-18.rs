//! Case 10 `return_closed` - PRESERVE_AND_WARN (see TRANSLATION_NOTES.md).
//! Go evaluates the return value (the pointer f), then runs D1, which closes the
//! same handle, so the caller receives a closed file. Rust follows the same order:
//! it evaluates the tail expression, then drops locals. The deferred close and the
//! returned value must alias one handle that can be closed in place, so the handle
//! is an `Rc<OsFile>`. The source bug is preserved, not repaired.
//!
//! WARNING (source behavior, deliberately not repaired): scenario returns a
//! closed handle, and the caller's use fails.
//! Translated from benchmarks/10_return_closed/main.go.

use std::cell::RefCell;
use std::fs::File;
use std::io;
use std::ops::Deref;
use std::os::fd::IntoRawFd;
use std::os::raw::c_int;
use std::rc::Rc;
use std::sync::OnceLock;
fn scenario() -> Option<Rc<OsFile>> {
    let f = os_open(path()); // RF:acquire A
    let Ok(f) = record_open("A", f) else { return None };
    let f = Rc::new(OsFile::new(f));
    let _d1 = DeferClosePtr::new("A", Rc::clone(&f)); // RF:defer D1 A
    Some(f) // return value evaluated first; then D1 closes the same handle
}

fn main() {
    parse_args();
    let f = scenario(); println!("returned"); if let Some(f) = f { use_file("A", &f) }
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

/// `defer closeFile(id, f)` with f a pointer: the pointer value (`P` = `&OsFile` or
/// `Rc<OsFile>`) is copied into the guard at registration, as Go does.
struct DeferClosePtr<P: Deref<Target = OsFile>> { id: String, f: P }
impl<P: Deref<Target = OsFile>> DeferClosePtr<P> {
    fn new(id: impl Into<String>, f: P) -> Self { DeferClosePtr { id: id.into(), f } }
}
impl<P: Deref<Target = OsFile>> Drop for DeferClosePtr<P> {
    fn drop(&mut self) { let _ = close_file(&self.id, &self.f); }
}
