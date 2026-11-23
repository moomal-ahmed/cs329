#![allow(dead_code)]

use std::ffi::OsString;
use std::fs::File;
use std::io::{self, Write};
use std::ops::Deref;
use std::os::fd::IntoRawFd;

extern "C" {
    fn close(fd: i32) -> i32;
    fn signal(sig: i32, handler: usize) -> usize;
    fn raise(sig: i32) -> i32;
}

/// Writes one line to stdout like Go's fmt.Println: unbuffered per line, write
/// errors ignored, except a broken pipe, which (as in Go) kills the process
/// with SIGPIPE.
macro_rules! say {
    ($($arg:tt)*) => { emit(format_args!($($arg)*)) };
}

fn emit(args: std::fmt::Arguments) {
    let mut out = io::stdout().lock();
    let res = out.write_fmt(args).and_then(|_| out.write_all(b"\n")).and_then(|_| out.flush());
    if let Err(e) = res {
        if e.kind() == io::ErrorKind::BrokenPipe {
            unsafe {
                signal(13, 0);
                raise(13);
            }
        }
    }
}

struct Args {
    path: OsString,
    mode: OsString,
}

impl Args {
    fn mode_is(&self, m: &str) -> bool {
        self.mode == *m
    }
}

/// Mirrors the Go program's argument handling, including its panic (exit
/// status 2) when no input path is given.
fn parse_args() -> Args {
    let mut it = std::env::args_os().skip(1);
    let path = match it.next() {
        Some(p) => p,
        None => {
            eprintln!("panic: pass input file path\n\ngoroutine 1 [running]:\nmain.main()");
            std::process::exit(2);
        }
    };
    let mode = it.next().unwrap_or_default();
    Args { path, mode }
}

fn open_rec(id: &str, path: &OsString) -> io::Result<File> {
    let res = File::open(path);
    say!("acquire {} {}", id, res.is_ok());
    res
}

/// Consumes the file and closes it explicitly so the close(2) result is observable.
fn close_file(id: &str, f: File) -> io::Result<()> {
    let fd = f.into_raw_fd();
    let res = if unsafe { close(fd) } == 0 { Ok(()) } else { Err(io::Error::last_os_error()) };
    say!("close {} {}", id, res.is_ok());
    res
}

fn use_file(id: &str, f: &File) {
    say!("use {} {}", id, f.metadata().is_ok());
}

/// Owns a file and closes it (reporting the result) when dropped: the Rust
/// counterpart of `defer closeFile(id, f)`.
struct Closer {
    id: String,
    file: Option<File>,
}

#[allow(dead_code)]
impl Closer {
    fn new(id: &str, file: File) -> Self {
        Closer { id: id.to_string(), file: Some(file) }
    }
    fn close(mut self) -> io::Result<()> {
        let f = self.file.take().expect("file present");
        close_file(&self.id, f)
    }
}

impl Deref for Closer {
    type Target = File;
    fn deref(&self) -> &File {
        self.file.as_ref().expect("file present")
    }
}

impl Drop for Closer {
    fn drop(&mut self) {
        if let Some(f) = self.file.take() {
            let _ = close_file(&self.id, f);
        }
    }
}

fn scenario(args: &Args) {
    let f = match open_rec("A", &args.path) {
        Ok(f) => f,
        Err(_) => return,
    };
    if args.mode_is("skip") {
        say!("skip");
        // The original never closes the file on this path: leak the descriptor.
        let _ = f.into_raw_fd();
        return;
    }
    use_file("A", &f);
    let _ = close_file("A", f);
}

fn main() {
    let args = parse_args();
    scenario(&args);
    say!("returned");
}
