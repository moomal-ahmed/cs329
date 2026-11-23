package main

import ("fmt"; "os")

var path, mode string

func scenario() {
f, err := os.Open(path) // RF:acquire A
if !recordOpen("A", f, err) { return }
g, err := os.Open(path) // RF:acquire B
if !recordOpen("B", g, err) { closeFile("A", f); return }
fn, gn := "A", "B"
defer func() { closeFile(fn, f); closeFile(gn, g) }() // RF:defer D1 A,B
if mode == "swap" { f, g = g, f; fn, gn = gn, fn }
useFile(fn, f)
}



func main() {
if len(os.Args) < 2 { panic("pass input file path") }
path = os.Args[1]
if len(os.Args) > 2 { mode = os.Args[2] }
scenario(); fmt.Println("returned")
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
