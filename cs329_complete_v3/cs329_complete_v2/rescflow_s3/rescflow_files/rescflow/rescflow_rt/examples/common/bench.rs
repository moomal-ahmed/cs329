//! The benchmarks' own instrumentation helpers (recordOpen, closeFile, useFile and the
//! path/mode globals), translated once and shared by the ported examples. These are program
//! code, not runtime: the runtime is `rescflow_rt`.
#![allow(dead_code)]
use rescflow_rt::{close, go_panic, OsFile};
use std::fs::File;
use std::io;
use std::sync::OnceLock;

static PATH: OnceLock<String> = OnceLock::new();
static MODE: OnceLock<String> = OnceLock::new();
pub fn path() -> &'static str { PATH.get().map(String::as_str).unwrap_or("") }
pub fn mode() -> &'static str { MODE.get().map(String::as_str).unwrap_or("") }

pub fn parse_args() {
    let args: Vec<String> = std::env::args().collect();
    if args.len() < 2 { go_panic("pass input file path") }
    PATH.set(args[1].clone()).unwrap();
    if args.len() > 2 { MODE.set(args[2].clone()).unwrap(); }
}
pub fn os_open(p: &str) -> io::Result<File> { File::open(p) }
/// Go recordOpen(id, f, err) bool
pub fn record_open(id: &str, r: io::Result<File>) -> io::Result<File> {
    println!("acquire {} {}", id, r.is_ok());
    r
}
/// Go closeFile(id, f) error, owned handle
pub fn close_file(id: &str, f: File) -> io::Result<()> {
    let err = close(f);
    println!("close {} {}", id, err.is_ok());
    err
}
/// Go closeFile(id, f) error, shared run-time-checked handle
pub fn close_file_shared(id: &str, f: &OsFile) -> io::Result<()> {
    let err = f.close();
    println!("close {} {}", id, err.is_ok());
    err
}
pub fn use_file(id: &str, f: &File) { println!("use {} {}", id, f.metadata().is_ok()); }
pub fn use_file_shared(id: &str, f: &OsFile) { println!("use {} {}", id, f.stat().is_ok()); }
