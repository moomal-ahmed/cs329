package main

import ("fmt"; "os")

var path = "input.txt"

func main() {
f, err := os.Open(path)
if err != nil { return }
defer f.Close(
fmt.Println("x")
}
