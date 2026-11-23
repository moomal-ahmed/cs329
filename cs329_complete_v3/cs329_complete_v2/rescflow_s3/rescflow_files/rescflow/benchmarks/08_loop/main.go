package main

import ("fmt"; "os")

var path, mode string

func scenario() {
for i := 0; i < 3; i++ {
id := fmt.Sprint(i)
f, err := os.Open(path) // RF:acquire A
if !recordOpen(id, f, err) { return }
defer closeFile(id, f) // RF:defer D1 A
fmt.Println("iteration", i)
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
