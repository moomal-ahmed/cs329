package main

import ("fmt"; "os")

var path = "input.txt"

func main() {
f, err := os.Open(path)
if err != nil { return }
defer f.Close()
if len(os.Args) > 5 { fmt.Println("bail"); os.Exit(1) }
fmt.Println("x")
}
