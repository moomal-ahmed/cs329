package main

import ("fmt"; "os"; "unsafe")

var path = "input.txt"

func main() {
f, err := os.Open(path)
if err != nil { return }
defer f.Close()
p := unsafe.Pointer(f)
fmt.Println(p != nil)
}
