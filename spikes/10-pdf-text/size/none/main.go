// Size probe: the baseline, a program that reads a file and prints its length.
package main

import (
	"fmt"
	"os"
)

func main() {
	b, _ := os.ReadFile(os.Args[1])
	fmt.Println(len(b))
}
