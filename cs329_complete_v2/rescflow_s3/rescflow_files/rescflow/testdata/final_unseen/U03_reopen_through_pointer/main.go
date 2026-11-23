package main

import ("fmt"; "os")

var path, mode string

func reopen(fp **os.File) bool {
g, err := os.Open(path) // RF:acquire B
if !recordOpen("B", g, err) { return false }
closeFile("A", *fp)
*fp = g
return true
}
func scenario() {
f, err := os.Open(path) // RF:acquire A
if !recordOpen("A", f, err) { return }
name := "A"
defer func() { closeFile(name, f) }() // RF:defer D1 A,B
if mode == "reopen" && reopen(&f) { name = "B" }
useFile(name, f)
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
