package main

import ("bufio"; "fmt"; "os")

var path = "input.txt"

func main() {
f, err := os.Open(path)
if err != nil { return }
defer f.Close()
r := bufio.NewReader(f)
fmt.Println(r.Size())
}
