// Programs written after the analyzer was built and never used to tune it (2026-11-23).
package main

import (
	"fmt"
	"io"
	"os"
)

// F1: the classic copy, with the close error of the destination joined into err.
func copyFile(dst, src string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()
	_, err = io.Copy(out, in)
	return err
}

// F2: explicit closes on some paths, but one early return forgets it.
func firstCheck(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return "", err
	}
	if st.Size() == 0 {
		return "", fmt.Errorf("empty file")
	}
	f.Close()
	return "ok", nil
}

// F3: the idiom for "defer inside a loop": one closure call per iteration.
func processAll(paths []string) {
	for _, p := range paths {
		func() {
			f, err := os.Open(p)
			if err != nil {
				return
			}
			defer f.Close()
			info, _ := f.Stat()
			fmt.Println(info.Name())
		}()
	}
}

// F4: a struct that owns the file; the caller defers the wrapper's Close.
type Logger struct{ f *os.File }

func NewLogger(p string) (*Logger, error) {
	f, err := os.Create(p)
	if err != nil {
		return nil, err
	}
	return &Logger{f: f}, nil
}
func (l *Logger) Log(s string) { l.f.WriteString(s) }
func (l *Logger) Close() error { return l.f.Close() }

func main() {
	_ = copyFile("/tmp/b", "/tmp/a")
	_, _ = firstCheck("/tmp/a")
	processAll([]string{"/tmp/a", "/tmp/b"})
	l, err := NewLogger("/tmp/x.log")
	if err != nil {
		return
	}
	defer l.Close()
	l.Log("hi")
}
