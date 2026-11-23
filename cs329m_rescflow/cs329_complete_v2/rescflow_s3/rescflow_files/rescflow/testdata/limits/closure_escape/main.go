package main

import ("fmt"; "os")

var path = "input.txt"

func opener() func() error {
f, err := os.Open(path)
if err != nil { return nil }
return func() error { return f.Close() }
}
func main() {
c := opener()
if c != nil { c() }
fmt.Println("x")
}
