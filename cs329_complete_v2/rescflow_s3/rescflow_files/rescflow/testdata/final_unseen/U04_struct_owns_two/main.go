package main

import ("fmt"; "os")

var path, mode string

func scenario() {
a, err := os.Open(path) // RF:acquire A
if !recordOpen("A", a, err) { return }
b, err := os.Open(path) // RF:acquire B
if !recordOpen("B", b, err) { closeFile("A", a); return }
p := &pair{a: a, b: b}
defer p.Close() // RF:defer D1 A,B
useFile("A", p.a)
useFile("B", p.b)
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

type pair struct{ a, b *os.File }
func (p *pair) Close() { closeFile("B", p.b); closeFile("A", p.a) }
