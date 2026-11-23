package main

import ("fmt"; "os")

var path = "input.txt"

func main() {
ch := make(chan *os.File, 1)
f, err := os.Open(path)
if err != nil { return }
ch <- f
g := <-ch
g.Close()
fmt.Println("x")
}
