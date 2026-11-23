//! Each test pins one Go behaviour the runtime must keep (benchmark case in brackets).

use rescflow_rt::*;
use std::cell::RefCell;
use std::fs::File;
use std::io::Write;
use std::os::fd::AsRawFd;
use std::path::PathBuf;

fn fixture(name: &str) -> PathBuf {
    let p = std::env::temp_dir().join(format!("rescflow_rt_{}_{}", std::process::id(), name));
    File::create(&p).unwrap().write_all(b"resource fixture\n").unwrap();
    p
}

fn open(name: &str) -> File {
    File::open(fixture(name)).unwrap()
}

#[test]
fn guards_close_in_reverse_declaration_order() {
    // [05 two_lifo] defer A; defer B  =>  close B, close A
    let log = RefCell::new(Vec::new());
    {
        let _a = DeferClose::with(open("a"), |f| {
            close(f).unwrap();
            log.borrow_mut().push("close A")
        });
        let _b = DeferClose::with(open("b"), |f| {
            close(f).unwrap();
            log.borrow_mut().push("close B")
        });
        log.borrow_mut().push("body");
    }
    assert_eq!(*log.borrow(), ["body", "close B", "close A"]);
}

#[test]
fn defer_stack_runs_loop_registrations_last_first() {
    // [08 loop] for i in 0..3 { defer close(i) }  =>  close 2, 1, 0 (a Vec of guards would give 0, 1, 2)
    let log = RefCell::new(Vec::new());
    {
        let mut defers = DeferStack::new();
        for i in 0..3 {
            let log = &log;
            defers.push(DeferClose::with(open(&format!("loop{i}")), move |f| {
                close(f).unwrap();
                log.borrow_mut().push(format!("close {i}"))
            }));
        }
        log.borrow_mut().push("after-loop".to_string());
    }
    assert_eq!(*log.borrow(), ["after-loop", "close 2", "close 1", "close 0"]);
}

#[test]
fn option_guard_runs_only_when_registered() {
    // [07 conditional] the defer is registered on one branch only
    for register in [true, false] {
        let log = RefCell::new(Vec::new());
        {
            let f = open("cond");
            let mut g = None;
            let mut f = Some(f);
            if register {
                g = Some(DeferClose::with(f.take().unwrap(), |f| {
                    close(f).unwrap();
                    log.borrow_mut().push("deferred close")
                }));
            }
            if let Some(f) = f {
                close(f).unwrap();
                log.borrow_mut().push("explicit close");
            }
            let _ = &g;
        }
        let want = if register { ["deferred close"] } else { ["explicit close"] };
        assert_eq!(*log.borrow(), want);
    }
}

#[test]
fn cell_closure_reads_the_variable_at_exit() {
    // [14 late_closure] defer func(){ close(f) }(); ...; f = g  =>  the defer closes g (B)
    let log = RefCell::new(Vec::new());
    {
        let f: RefCell<Option<(&str, File)>> = RefCell::new(Some(("A", open("cellA"))));
        let _d = Defer::new(|| {
            let (name, file) = f.borrow_mut().take().unwrap();
            close(file).unwrap();
            log.borrow_mut().push(format!("deferred close {name}"));
        });
        let old = f.replace(Some(("B", open("cellB")))).unwrap();
        close(old.1).unwrap();
        log.borrow_mut().push(format!("explicit close {}", old.0));
    }
    assert_eq!(*log.borrow(), ["explicit close A", "deferred close B"]);
}

#[test]
fn arguments_are_fixed_at_registration() {
    // [13 eager_argument] defer close(f); f = g  =>  the defer still closes A
    let log = RefCell::new(Vec::new());
    {
        let mut name = "A";
        let a = open("eagerA");
        let _d = DeferClose::with(a, |f| {
            close(f).unwrap();
            log.borrow_mut().push("close A")
        });
        name = if name == "A" { "B" } else { name }; // reassignment does not reach the guard
        log.borrow_mut().push(if name == "B" { "reassigned" } else { "?" });
    }
    assert_eq!(*log.borrow(), ["reassigned", "close A"]);
}

#[test]
fn close_releases_the_descriptor_and_reports_errors() {
    let f = open("fd");
    let fd = f.as_raw_fd();
    assert!(close(f).is_ok());
    if cfg!(target_os = "linux") {
        assert!(!PathBuf::from(format!("/proc/self/fd/{fd}")).exists(), "descriptor still open");
    }
}

#[test]
fn os_file_keeps_go_double_close_and_use_after_close() {
    // [16 / 19] second Close fails with "file already closed"; [10 / 18] use after close fails
    let f = OsFile::new(open("osfile"));
    assert!(f.stat().is_ok());
    assert!(f.close().is_ok());
    assert!(f.is_closed());
    let second = f.close().unwrap_err();
    assert_eq!(second.to_string(), "file already closed");
    assert!(f.stat().is_err());
}

#[test]
fn into_inner_cancels_the_cleanup() {
    // plan B: ownership moves out on one path, so the guard must not close it
    let log = RefCell::new(Vec::new());
    let file = {
        let g = DeferClose::with(open("transfer"), |_f| log.borrow_mut().push("closed"));
        g.into_inner()
    };
    assert!(log.borrow().is_empty());
    assert!(close(file).is_ok());
}

#[test]
fn guards_run_while_a_recovered_panic_unwinds() {
    // [17 panic_unwind] defer recover; defer close(A); panic  =>  close A, then recovered
    let log = RefCell::new(Vec::new());
    let r = recover(|| {
        let _d = DeferClose::with(open("panic"), |f| {
            close(f).unwrap();
            log.borrow_mut().push("close A")
        });
        log.borrow_mut().push("panic");
        std::panic::panic_any("test");
    });
    if r.is_err() {
        log.borrow_mut().push("recovered");
    }
    assert_eq!(*log.borrow(), ["panic", "close A", "recovered"]);
}

#[test]
fn defer_cancel_skips_the_action() {
    let log = RefCell::new(Vec::<&str>::new());
    Defer::new(|| log.borrow_mut().push("ran")).cancel();
    assert!(log.borrow().is_empty());
}
