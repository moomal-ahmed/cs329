package main

import ("fmt"; "os")

var path, mode string

func scenario() {
ids := []string{"0", "1", "2"}
for i := 0; i < 3; i++ {
f, err := os.Open(path) // RF:acquire A
if !recordOpen(ids[i], f, err) { return }
if i%2 == 0 {
defer closeFile(ids[i], f) // RF:defer D1 A
} else {
closeFile(ids[i], f)
}
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
