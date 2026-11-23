package main

import ("fmt"; "os")

var path = "input.txt"

func run(closer func(*os.File) error) {
f, err := os.Open(path)
if err != nil { return }
defer closer(f)
fmt.Println("x")
}
func main() { run((*os.File).Close) }
