package main

import ("fmt"; "os")

var path, mode string

func scenario() {
ids := []string{"0", "1"}
for i := 0; i < 2; i++ {
f, err := os.Open(path) // RF:acquire A
if !recordOpen(ids[i]+"a", f, err) { return }
defer closeFile(ids[i]+"a", f) // RF:defer D1 A
g, err := os.Open(path) // RF:acquire B
if !recordOpen(ids[i]+"b", g, err) { return }
defer closeFile(ids[i]+"b", g) // RF:defer D2 B
}
fmt.Println("after-loop")
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
