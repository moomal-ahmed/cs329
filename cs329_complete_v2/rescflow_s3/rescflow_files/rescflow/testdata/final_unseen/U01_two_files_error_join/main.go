package main

import ("fmt"; "os")

var path, mode string

func scenario() (err error) {
src, err := os.Open(path) // RF:acquire A
if !recordOpen("A", src, err) { return err }
defer closeFile("A", src) // RF:defer D1 A
dst, err := os.Open(path) // RF:acquire B
if !recordOpen("B", dst, err) { return err }
defer func() { if cerr := closeFile("B", dst); err == nil { err = cerr } }() // RF:defer D2 B
useFile("A", src)
useFile("B", dst)
return nil
}



func main() {
if len(os.Args) < 2 { panic("pass input file path") }
path = os.Args[1]
if len(os.Args) > 2 { mode = os.Args[2] }
err := scenario(); fmt.Println("result-error", err != nil)
}

func recordOpen(id string, f *os.File, err error) bool {
 fmt.Println("acquire", id, err == nil)
 return err == nil
}
func closeFile(id string, f *os.File) error {
 err := f.Close()
 fmt.Println("close", id, err == nil)
 return err
}
func useFile(id string, f *os.File) {
 _, err := f.Stat()
 fmt.Println("use", id, err == nil)
}
