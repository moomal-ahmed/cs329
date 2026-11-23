use std::cell::{RefCell};
use std::error::Error as StdError;
use std::rc::{Rc};


thread_local! {
    static __GO_OS_ARGS: std::rc::Rc<std::cell::RefCell<Option<Vec<String>>>> =
        std::rc::Rc::new(std::cell::RefCell::new(Some(std::env::args().collect::<Vec<String>>())));
}

fn go_os_args() -> std::rc::Rc<std::cell::RefCell<Option<Vec<String>>>> {
    __GO_OS_ARGS.with(|args| args.clone())
}

fn __go_next_external_interface_id() -> usize {
    static NEXT_ID: std::sync::atomic::AtomicUsize = std::sync::atomic::AtomicUsize::new(1);
    NEXT_ID.fetch_add(1, std::sync::atomic::Ordering::Relaxed)
}



#[derive(Debug, Clone, Default, PartialEq, Eq, PartialOrd, Ord)]
pub struct fs_FileInfo {
    pub name: String,
    pub is_dir: bool,
    pub size: i64,
}

impl std::fmt::Display for fs_FileInfo {
    fn fmt(&self, f: &mut std::fmt::Formatter) -> std::fmt::Result {
        write!(f, "<fs_FileInfo>")
    }
}


impl fs_FileInfo {
    pub fn downcast_ref<T: 'static>(&self) -> Option<&T> {
        None
    }
    pub fn name(&self) -> Rc<RefCell<Option<String>>> {
        Rc::new(RefCell::new(Some::<String>(self.name.clone())))
    }
    pub fn size(&self) -> i64 {
        self.size
    }
    pub fn is_dir(&self) -> bool {
        self.is_dir
    }
}


#[derive(Debug, Clone)]
pub struct os_File {
    pub __go_data: std::sync::Arc<std::sync::Mutex<Vec<u8>>>,
    pub __go_closed: std::sync::Arc<std::sync::atomic::AtomicBool>,
    pub __go_wait_for_close: bool,
}

impl Default for os_File {
    fn default() -> Self {
        Self {
            __go_data: std::sync::Arc::new(std::sync::Mutex::new(Vec::new())),
            __go_closed: std::sync::Arc::new(std::sync::atomic::AtomicBool::new(false)),
            __go_wait_for_close: false,
        }
    }
}

impl std::fmt::Display for os_File {
    fn fmt(&self, f: &mut std::fmt::Formatter) -> std::fmt::Result {
        write!(f, "<os_File>")
    }
}

impl PartialEq for os_File {
    fn eq(&self, other: &Self) -> bool {
        std::sync::Arc::ptr_eq(&self.__go_data, &other.__go_data)
    }
}

impl Eq for os_File {}

impl os_File {
    pub fn downcast_ref<T: 'static>(&self) -> Option<&T> {
        None
    }

    pub fn __go_write_bytes(&self, data: &[u8]) {
        self.__go_data.lock().unwrap().extend_from_slice(data);
    }

    pub fn __go_read_all(&self) -> Vec<u8> {
        self.__go_data.lock().unwrap().clone()
    }

    pub fn close(&self) -> Rc<RefCell<Option<Box<dyn StdError>>>> {
        self.__go_closed.store(true, std::sync::atomic::Ordering::SeqCst);
        Rc::new(RefCell::new(None::<Box<dyn StdError>>))
    }
    // RESCFLOW PATCH (not Go2Rust output): minimal Stat stand-in for useFile.
    pub fn stat(&self) -> (Rc<RefCell<Option<fs_FileInfo>>>, Rc<RefCell<Option<Box<dyn StdError>>>>) {
        if self.__go_closed.load(std::sync::atomic::Ordering::SeqCst) {
            (Rc::new(RefCell::new(None)), Rc::new(RefCell::new(Some(Box::new(std::io::Error::other("file already closed")) as Box<dyn StdError>))))
        } else {
            (Rc::new(RefCell::new(Some(fs_FileInfo::default()))), Rc::new(RefCell::new(None::<Box<dyn StdError>>)))
        }
    }

    pub fn write<T0: 'static>(&self, arg0: T0) -> (Rc<RefCell<Option<i32>>>, Rc<RefCell<Option<Box<dyn StdError>>>>) {
        let bytes = if let Some(v) = (&arg0 as &dyn std::any::Any).downcast_ref::<Vec<u8>>() {
            v.clone()
        } else if let Some(v) = (&arg0 as &dyn std::any::Any).downcast_ref::<Rc<RefCell<Option<Vec<u8>>>>>() {
            v.borrow().as_ref().cloned().unwrap_or_default()
        } else {
            Vec::new()
        };
        let n = bytes.len() as i32;
        self.__go_write_bytes(&bytes);
        (Rc::new(RefCell::new(Some::<i32>(n))), Rc::new(RefCell::new(None::<Box<dyn StdError>>)))
    }

    pub fn write_string<T0: 'static>(&self, arg0: T0) -> (Rc<RefCell<Option<i32>>>, Rc<RefCell<Option<Box<dyn StdError>>>>) {
        let value = if let Some(v) = (&arg0 as &dyn std::any::Any).downcast_ref::<String>() {
            v.clone()
        } else if let Some(v) = (&arg0 as &dyn std::any::Any).downcast_ref::<&str>() {
            (*v).to_string()
        } else if let Some(v) = (&arg0 as &dyn std::any::Any).downcast_ref::<Rc<RefCell<Option<String>>>>() {
            v.borrow().as_ref().cloned().unwrap_or_default()
        } else {
            String::new()
        };
        let bytes = value.into_bytes();
        let n = bytes.len() as i32;
        self.__go_write_bytes(&bytes);
        (Rc::new(RefCell::new(Some::<i32>(n))), Rc::new(RefCell::new(None::<Box<dyn StdError>>)))
    }

    pub fn read<T0>(&self, _arg0: T0) -> (Rc<RefCell<Option<i32>>>, Rc<RefCell<Option<Box<dyn StdError>>>>) {
        (Rc::new(RefCell::new(Some::<i32>(0))), Rc::new(RefCell::new(None::<Box<dyn StdError>>)))
    }

    pub fn read_at<T0: 'static, T1: 'static>(&self, arg0: T0, arg1: T1) -> (Rc<RefCell<Option<i32>>>, Rc<RefCell<Option<Box<dyn StdError>>>>) {
        let offset = if let Some(v) = (&arg1 as &dyn std::any::Any).downcast_ref::<i64>() {
            *v
        } else if let Some(v) = (&arg1 as &dyn std::any::Any).downcast_ref::<Rc<RefCell<Option<i64>>>>() {
            v.borrow().as_ref().copied().unwrap_or_default()
        } else {
            0
        };
        let data = self.__go_read_all();
        let mut n = 0i32;
        if offset >= 0 {
            let start = offset as usize;
            if start < data.len() {
                if let Some(v) = (&arg0 as &dyn std::any::Any).downcast_ref::<Rc<RefCell<Option<Vec<u8>>>>>() {
                    let mut guard = v.borrow_mut();
                    if let Some(target) = guard.as_mut() {
                        let count = std::cmp::min(target.len(), data.len() - start);
                        target[..count].copy_from_slice(&data[start..start + count]);
                        n = count as i32;
                    }
                }
            }
        }
        (Rc::new(RefCell::new(Some::<i32>(n))), Rc::new(RefCell::new(None::<Box<dyn StdError>>)))
    }
}


pub mod os {
    use super::*;
    use std::path::Path;

    pub trait GoStringArg {
        fn into_go_string(self) -> String;
    }

    impl GoStringArg for String {
        fn into_go_string(self) -> String {
            self
        }
    }

    impl<'a> GoStringArg for &'a str {
        fn into_go_string(self) -> String {
            self.to_string()
        }
    }

    impl<'a> GoStringArg for &'a String {
        fn into_go_string(self) -> String {
            self.clone()
        }
    }

    impl GoStringArg for Rc<RefCell<Option<String>>> {
        fn into_go_string(self) -> String {
            self.borrow().as_ref().cloned().unwrap_or_default()
        }
    }

    type GoError = Rc<RefCell<Option<Box<dyn std::error::Error>>>>;

    fn no_error() -> GoError {
        Rc::new(RefCell::new(None))
    }

    fn io_error(err: std::io::Error) -> GoError {
        Rc::new(RefCell::new(Some(Box::new(err))))
    }

    pub fn open<T0: GoStringArg>(_arg0: T0) -> (Rc<RefCell<Option<os_File>>>, Rc<RefCell<Option<Box<dyn StdError>>>>) {
        let __go_path = _arg0.into_go_string();
        match std::fs::read(&__go_path) {
            Ok(data) => {
                let file = os_File { __go_data: std::sync::Arc::new(std::sync::Mutex::new(data)), __go_closed: std::sync::Arc::new(std::sync::atomic::AtomicBool::new(false)), __go_wait_for_close: false };
                (Rc::new(RefCell::new(Some::<os_File>(file))), no_error())
            }
            Err(err) => (Rc::new(RefCell::new(None::<os_File>)), io_error(err)),
        }
    }
}


pub(crate) struct GoGlobal<T> {
    value: std::cell::UnsafeCell<Option<T>>,
}
unsafe impl<T> Sync for GoGlobal<T> {}
impl<T> GoGlobal<T> {
    pub(crate) const fn new() -> Self {
        Self { value: std::cell::UnsafeCell::new(None) }
    }
    pub(crate) fn borrow(&'static self) -> &'static Option<T> {
        unsafe { &*self.value.get() }
    }
    pub(crate) fn borrow_mut(&'static self) -> &'static mut Option<T> {
        unsafe { &mut *self.value.get() }
    }
    pub(crate) fn clone(&'static self) -> std::rc::Rc<std::cell::RefCell<Option<T>>> where T: Clone {
        std::rc::Rc::new(std::cell::RefCell::new(self.borrow().clone()))
    }
}

pub(crate) static path: GoGlobal<String> = GoGlobal::new();

pub(crate) static mode: GoGlobal<String> = GoGlobal::new();


fn __go_init_globals() {
    *path.borrow_mut() = Some(String::new());
    *mode.borrow_mut() = Some(String::new());
}


pub fn scenario() -> Rc<RefCell<Option<os_File>>> {
    let mut __defer_stack: Vec<Box<dyn FnOnce()>> = Vec::new();

    let (mut f, mut err) = os::open({ let __arg_holder = path.clone(); let __arg_guard = __arg_holder.borrow(); (*__arg_guard.as_ref().unwrap()).clone() });
    if !record_open(Rc::new(RefCell::new(Some("A".to_string()))), f.clone(), err.clone()) {
        {
        // Execute deferred functions
        while let Some(f) = __defer_stack.pop() {
            f();
        }
        return Rc::new(RefCell::new(None));
    }
    }
    let f_defer_captured = f.clone(); __defer_stack.push(Box::new(move || {
        close_file(Rc::new(RefCell::new(Some("A".to_string()))), f_defer_captured.clone());
    }));
    {
        // Execute deferred functions
        while let Some(f) = __defer_stack.pop() {
            f();
        }
        return f.clone();
    }
}

fn main() {
    __go_init_all();
    if (({ let __len_target = { let __field = go_os_args().clone(); __field }; let __len_guard = __len_target.borrow(); __len_guard.as_ref().map(|__v| __v.len()).unwrap_or(0) }) as i32) < (2 as i32) {
        panic!("pass input file path");
    }
    { let new_val = (*go_os_args().borrow().as_ref().unwrap())[(1) as usize].clone(); *path.borrow_mut() = Some(new_val); };
    if (({ let __len_target = { let __field = go_os_args().clone(); __field }; let __len_guard = __len_target.borrow(); __len_guard.as_ref().map(|__v| __v.len()).unwrap_or(0) }) as i32) > (2 as i32) {
        { let new_val = (*go_os_args().borrow().as_ref().unwrap())[(2) as usize].clone(); *mode.borrow_mut() = Some(new_val); };
    }
    let mut f = scenario();
    println!("{}", format!("{}", "returned".to_string()));
    if { let __nil_result = (*f.borrow()).is_some(); __nil_result } {
        use_file(Rc::new(RefCell::new(Some("A".to_string()))), f.clone());
    }
}

pub fn record_open(id: Rc<RefCell<Option<String>>>, f: Rc<RefCell<Option<os_File>>>, err: Rc<RefCell<Option<Box<dyn StdError>>>>) -> bool {
    println!("{} {} {}", format!("{}", "acquire".to_string()), format!("{}", { let __v = (*id.borrow().as_ref().unwrap()).clone(); __v }), format!("{}", { let __nil_result = (*err.borrow()).is_none(); __nil_result }));
    { let __nil_result = (*err.borrow()).is_none(); __nil_result }
}

pub fn close_file(id: Rc<RefCell<Option<String>>>, f: Rc<RefCell<Option<os_File>>>) -> Rc<RefCell<Option<Box<dyn StdError>>>> {
    let mut err = (*f.borrow_mut().as_mut().unwrap()).close();
    println!("{} {} {}", format!("{}", "close".to_string()), format!("{}", { let __v = (*id.borrow().as_ref().unwrap()).clone(); __v }), format!("{}", { let __nil_result = (*err.borrow()).is_none(); __nil_result }));
    return err.clone();
}

pub fn use_file(id: Rc<RefCell<Option<String>>>, f: Rc<RefCell<Option<os_File>>>) {
    let (_, mut err) = (*f.borrow_mut().as_mut().unwrap()).stat();
    println!("{} {} {}", format!("{}", "use".to_string()), format!("{}", { let __v = (*id.borrow().as_ref().unwrap()).clone(); __v }), format!("{}", { let __nil_result = (*err.borrow()).is_none(); __nil_result }));
}

pub(crate) fn __go_init_all() {
    self::__go_init_globals();
}
