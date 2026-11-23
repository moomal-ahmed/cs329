package main

import ("fmt"; "os")

var path = "input.txt"

func closeAll(fs []*os.File, i int) {
if i == len(fs) { return }
defer fs[i].Close()
closeAll(fs, i+1)
}
func walk(f *os.File, n int) {
if n == 0 { f.Close(); return }
walk(f, n-1)
}
func main() {
f, err := os.Open(path)
if err != nil { return }
walk(f, 3)
fmt.Println("x")
}
