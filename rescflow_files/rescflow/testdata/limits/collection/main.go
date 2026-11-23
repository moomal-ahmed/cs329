package main

import ("fmt"; "os")

var path = "input.txt"

func main() {
var files []*os.File
for i := 0; i < 2; i++ {
f, err := os.Open(path)
if err != nil { return }
files = append(files, f)
}
for _, f := range files { f.Close() }
fmt.Println("closed")
}
