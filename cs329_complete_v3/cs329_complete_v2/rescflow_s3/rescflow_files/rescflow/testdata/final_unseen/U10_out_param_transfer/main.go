package main

import ("fmt"; "os")

var path, mode string

func scenario(out **os.File) {
f, err := os.Open(path) // RF:acquire A
if !recordOpen("A", f, err) { return }
if mode == "give" { *out = f; return }
defer closeFile("A", f) // RF:defer D1 A
useFile("A", f)
}



func main() {
if len(os.Args) < 2 { panic("pass input file path") }
path = os.Args[1]
if len(os.Args) > 2 { mode = os.Args[2] }
var g *os.File; scenario(&g); fmt.Println("returned"); if g != nil { useFile("A", g); closeFile("A", g) }
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
