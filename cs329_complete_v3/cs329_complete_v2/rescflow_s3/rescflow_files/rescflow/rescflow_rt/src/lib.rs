//! `rescflow_rt`: the small runtime that RescFlow's generated Rust uses to express Go's
//! deferred resource cleanup with Rust ownership.
//!
//! Extracted from the LLM-written translations of the RescFlow benchmarks
//! (`Claude_translations/`), where each file carried its own copy of these types, and
//! generalized so that a guard runs any cleanup (the copies hard-wired the benchmarks' trace
//! printing). One type per step-3 plan:
//!
//! | Plan (step 3)          | Type                                   | Go behaviour it keeps                              |
//! |------------------------|----------------------------------------|----------------------------------------------------|
//! | `OWNED_GUARD`          | [`DeferClose`]                         | cleanup at function exit; arguments fixed at `defer`; LIFO via reverse drop order |
//! | `OPTION_GUARD`         | `Option<DeferClose>`                   | a defer registered on some paths only             |
//! | `DEFER_STACK`          | [`DeferStack`]                         | a defer inside a loop: every registration runs, last first |
//! | `CELL_CLOSURE_GUARD`   | [`Defer`] + `RefCell<Option<File>>`    | a deferred closure reads a variable when it runs  |
//! | `RESULT_SLOT_GUARD`    | [`Defer`] + [`close`]                  | the close error is observed (written to a result) |
//! | `DYN_GUARD`            | [`Defer`] over a `Box<dyn ...>`        | a deferred interface call                          |
//! | review (C) cases kept  | [`OsFile`]                             | use after close / double close fail at run time, as in Go |
//! | `+ CATCH_UNWIND`       | [`recover`]                            | guards run during a panic; `recover()` stops it   |
//!
//! Unix only: [`close`] calls `close(2)` directly, because `Drop for File` discards close errors.

use std::any::Any;
use std::cell::RefCell;
use std::fs::File;
use std::io;
use std::ops::{Deref, DerefMut};

// ------------------------------------------------------------------ closing

/// Close a file and report the result. `Drop for File` closes too, but discards the error;
/// Go's `Close` returns it. Like Go, `EINTR` counts as success (the descriptor is gone).
#[cfg(unix)]
pub fn close(f: File) -> io::Result<()> {
    use std::os::fd::IntoRawFd;
    use std::os::raw::c_int;
    extern "C" {
        #[link_name = "close"]
        fn c_close(fd: c_int) -> c_int;
    }
    let fd = f.into_raw_fd();
    // SAFETY: `into_raw_fd` transferred ownership of `fd` to us; it is closed exactly once here.
    if unsafe { c_close(fd) } == 0 {
        return Ok(());
    }
    let e = io::Error::last_os_error();
    if e.kind() == io::ErrorKind::Interrupted {
        Ok(())
    } else {
        Err(e)
    }
}

/// The error a second `Close` returns in Go (`os.ErrClosed`'s text).
pub fn err_closed() -> io::Error {
    io::Error::other("file already closed")
}

fn close_discarding(f: File) {
    // A deferred call's results are discarded in Go.
    let _ = close(f);
}

// ------------------------------------------------------------------ Defer

/// Runs a closure when dropped: Go's `defer func() { ... }()`.
///
/// Whatever the closure reads is read when it runs, not when the guard is created, so a
/// closure that reads a `RefCell` sees the value the cell holds at function exit
/// (plan `CELL_CLOSURE_GUARD`).
#[must_use = "a Defer runs when it is dropped; `let _ = Defer::new(..)` drops it at once, use `let _g = ..`"]
pub struct Defer<F: FnOnce()>(Option<F>);

impl<F: FnOnce()> Defer<F> {
    pub fn new(f: F) -> Self {
        Defer(Some(f))
    }
    /// Do not run the deferred closure.
    pub fn cancel(mut self) {
        self.0 = None;
    }
}

impl<F: FnOnce()> Drop for Defer<F> {
    fn drop(&mut self) {
        if let Some(f) = self.0.take() {
            f()
        }
    }
}

// ------------------------------------------------------------------ DeferClose

/// An owned file plus the cleanup that runs when the guard drops: Go's `defer closeFile(f)`.
///
/// The file (and anything the cleanup captures) is moved in when the guard is created, which
/// is when Go evaluates a deferred call's arguments. Guards drop in reverse declaration order,
/// which is Go's LIFO defer order. Declare the guard at function scope even when Go's `defer`
/// is inside a block: Go runs it at function exit, Rust would drop it at block exit.
#[must_use = "the cleanup runs when the guard is dropped"]
pub struct DeferClose<F: FnOnce(File) = fn(File)> {
    file: Option<File>,
    cleanup: Option<F>,
}

impl DeferClose<fn(File)> {
    /// Close with [`close`] at drop; the close error is discarded, as Go discards a deferred
    /// call's results.
    pub fn new(f: File) -> Self {
        DeferClose { file: Some(f), cleanup: Some(close_discarding) }
    }
}

impl<F: FnOnce(File)> DeferClose<F> {
    /// Run `cleanup(file)` at drop (e.g. the program's own `closeFile` helper).
    pub fn with(f: File, cleanup: F) -> Self {
        DeferClose { file: Some(f), cleanup: Some(cleanup) }
    }
    /// Cancel the deferred cleanup and take the file back, for a path where ownership moves
    /// elsewhere (plan B: transfer on one path, guard on another).
    pub fn into_inner(mut self) -> File {
        self.cleanup = None;
        self.file.take().expect("DeferClose holds its file until dropped")
    }
}

impl<F: FnOnce(File)> Deref for DeferClose<F> {
    type Target = File;
    fn deref(&self) -> &File {
        self.file.as_ref().expect("DeferClose holds its file until dropped")
    }
}

impl<F: FnOnce(File)> DerefMut for DeferClose<F> {
    fn deref_mut(&mut self) -> &mut File {
        self.file.as_mut().expect("DeferClose holds its file until dropped")
    }
}

impl<F: FnOnce(File)> Drop for DeferClose<F> {
    fn drop(&mut self) {
        if let (Some(f), Some(c)) = (self.file.take(), self.cleanup.take()) {
            c(f)
        }
    }
}

// ------------------------------------------------------------------ DeferOwn

/// Owns a value (typically `Option<File>`, Go's nil-able `*os.File`) and runs `cleanup(value)`
/// when dropped. The generator uses it for `OWNED_GUARD`: the deferred call's arguments are
/// evaluated at the `defer`, the resource moves in, and later uses read it through `Deref`.
#[must_use = "the cleanup runs when the guard is dropped"]
pub struct DeferOwn<T, F: FnOnce(T)> {
    value: Option<T>,
    cleanup: Option<F>,
}

impl<T, F: FnOnce(T)> DeferOwn<T, F> {
    pub fn new(value: T, cleanup: F) -> Self {
        DeferOwn { value: Some(value), cleanup: Some(cleanup) }
    }
    /// Cancel the cleanup and take the value back (ownership moves elsewhere).
    pub fn into_inner(mut self) -> T {
        self.cleanup = None;
        self.value.take().expect("DeferOwn holds its value until dropped")
    }
}

impl<T, F: FnOnce(T)> Deref for DeferOwn<T, F> {
    type Target = T;
    fn deref(&self) -> &T {
        self.value.as_ref().expect("DeferOwn holds its value until dropped")
    }
}

impl<T, F: FnOnce(T)> DerefMut for DeferOwn<T, F> {
    fn deref_mut(&mut self) -> &mut T {
        self.value.as_mut().expect("DeferOwn holds its value until dropped")
    }
}

impl<T, F: FnOnce(T)> Drop for DeferOwn<T, F> {
    fn drop(&mut self) {
        if let (Some(v), Some(c)) = (self.value.take(), self.cleanup.take()) {
            c(v)
        }
    }
}

// ------------------------------------------------------------------ DeferStack

/// Deferred actions registered in a loop, run last-first when the stack drops: Go's `defer`
/// inside a `for`. A plain `Vec` of guards would drop first-to-last, the reverse of Go.
#[must_use = "the deferred actions run when the stack is dropped"]
#[derive(Default)]
pub struct DeferStack<'a> {
    pending: Vec<Pending<'a>>,
}

enum Pending<'a> {
    Action(Box<dyn FnOnce() + 'a>),
    File(Option<File>, Box<dyn FnOnce(Option<File>) + 'a>),
}

impl<'a> DeferStack<'a> {
    pub fn new() -> Self {
        DeferStack { pending: Vec::new() }
    }
    /// Register any deferred action.
    pub fn defer(&mut self, f: impl FnOnce() + 'a) {
        self.pending.push(Pending::Action(Box::new(f)));
    }
    /// Register a deferred cleanup that owns a (nil-able) file; returns a slot to read the
    /// file through while it is pending (Go code may keep using it after the `defer`).
    pub fn push_file(&mut self, f: Option<File>, cleanup: impl FnOnce(Option<File>) + 'a) -> usize {
        self.pending.push(Pending::File(f, Box::new(cleanup)));
        self.pending.len() - 1
    }
    /// The file a pending `push_file` entry owns.
    pub fn file(&self, slot: usize) -> Option<&File> {
        match self.pending.get(slot) {
            Some(Pending::File(f, _)) => f.as_ref(),
            _ => None,
        }
    }
    /// Register a guard; it drops (runs its cleanup) when its turn comes.
    pub fn push<F: FnOnce(File) + 'a>(&mut self, g: DeferClose<F>) {
        self.defer(move || drop(g));
    }
    pub fn len(&self) -> usize {
        self.pending.len()
    }
    pub fn is_empty(&self) -> bool {
        self.pending.is_empty()
    }
}

impl Drop for DeferStack<'_> {
    fn drop(&mut self) {
        while let Some(p) = self.pending.pop() {
            match p {
                Pending::Action(f) => f(),
                Pending::File(file, f) => f(file),
            }
        }
    }
}

// ------------------------------------------------------------------ OsFile

/// A shared, run-time-checked handle for the cases RescFlow classifies C but keeps as Go
/// behaves (use after close, double close, returning a closed handle). A second `close`
/// returns [`err_closed`], and every use after close fails, as `*os.File` does in Go.
/// Prefer an owned `File` everywhere else: there, use after close does not compile.
pub struct OsFile {
    file: RefCell<Option<File>>,
}

impl OsFile {
    pub fn new(f: File) -> Self {
        OsFile { file: RefCell::new(Some(f)) }
    }
    pub fn close(&self) -> io::Result<()> {
        match self.file.borrow_mut().take() {
            Some(f) => close(f),
            None => Err(err_closed()),
        }
    }
    pub fn is_closed(&self) -> bool {
        self.file.borrow().is_none()
    }
    /// Use the open file; fails with [`err_closed`] after close.
    pub fn with<R>(&self, op: impl FnOnce(&File) -> io::Result<R>) -> io::Result<R> {
        match self.file.borrow().as_ref() {
            Some(f) => op(f),
            None => Err(err_closed()),
        }
    }
    pub fn stat(&self) -> io::Result<std::fs::Metadata> {
        self.with(|f| f.metadata())
    }
}

// ------------------------------------------------------------------ panics

/// Run `body`, stopping a panic the way a Go deferred `recover()` does: guards inside `body`
/// drop while the panic unwinds, then `Err(payload)` is returned. The default panic message is
/// suppressed meanwhile, because a recovered Go panic prints nothing. Requires `panic = "unwind"`.
/// The panic hook is process-global: do not call this from several threads at once.
pub fn recover<R>(body: impl FnOnce() -> R) -> Result<R, Box<dyn Any + Send>> {
    let prev = std::panic::take_hook();
    std::panic::set_hook(Box::new(|_| {}));
    let r = std::panic::catch_unwind(std::panic::AssertUnwindSafe(body));
    std::panic::set_hook(prev);
    r
}

/// Go's unrecovered `panic(msg)`: the message on stderr and exit status 2 (Rust's own `panic!`
/// exits with 101, a different observable result).
pub fn go_panic(msg: &str) -> ! {
    eprintln!("panic: {msg}");
    std::process::exit(2)
}

// ------------------------------------------------------------------ Go library shims

/// Shims for the parts of Go's standard library the generator maps directly. A Go `*os.File`
/// is `Option<File>` (nil-able); an `error` is [`go::Error`] (`None` = nil). `Close` and `Stat`
/// on a nil file fail instead of panicking, as in Go.
pub mod go {
    use std::fs::File;
    use std::io;
    use std::rc::Rc;

    /// Go `error`: `None` is nil. `Rc` because Go copies error values freely.
    pub type Error = Option<Rc<io::Error>>;

    fn invalid() -> Error {
        Some(Rc::new(io::Error::from(io::ErrorKind::InvalidInput)))
    }

    /// `os.Open(path)`
    pub fn open(path: &str) -> (Option<File>, Error) {
        match File::open(path) {
            Ok(f) => (Some(f), None),
            Err(e) => (None, Some(Rc::new(e))),
        }
    }

    /// `f.Close()`; the descriptor is closed with [`super::close`] so its error is reported.
    pub fn close(f: Option<File>) -> Error {
        match f {
            Some(f) => super::close(f).err().map(Rc::new),
            None => invalid(),
        }
    }

    /// `f.Stat()`
    pub fn stat(f: Option<&File>) -> (Option<std::fs::Metadata>, Error) {
        match f {
            Some(f) => match f.metadata() {
                Ok(m) => (Some(m), None),
                Err(e) => (None, Some(Rc::new(e))),
            },
            None => (None, invalid()),
        }
    }

    /// `os.Args`
    pub fn args() -> Vec<String> {
        std::env::args().collect()
    }

    /// `panic(msg)`: unwinds, so deferred guards run and an enclosing [`super::recover`] can stop it.
    pub fn panic(msg: String) -> ! {
        std::panic::panic_any(msg)
    }

    /// Runs Go's `main`: an unrecovered panic prints `panic: <msg>` and exits with status 2,
    /// as Go does (Rust's default would print its own message and exit with 101).
    pub fn main(body: impl FnOnce()) {
        let prev = std::panic::take_hook();
        std::panic::set_hook(Box::new(|_| {}));
        let r = std::panic::catch_unwind(std::panic::AssertUnwindSafe(body));
        std::panic::set_hook(prev);
        if let Err(p) = r {
            let msg = p
                .downcast_ref::<String>()
                .cloned()
                .or_else(|| p.downcast_ref::<&str>().map(|s| s.to_string()))
                .unwrap_or_else(|| "panic".to_string());
            eprintln!("panic: {msg}");
            std::process::exit(2)
        }
    }
}
