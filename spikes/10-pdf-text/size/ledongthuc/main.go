// Size probe: what github.com/ledongthuc/pdf adds to a binary.
package main

import (
	"fmt"
	"os"

	"github.com/ledongthuc/pdf"
)

func main() {
	f, r, err := pdf.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer f.Close()
	t, _ := r.Page(1).GetPlainText(nil)
	fmt.Println(len(t))
}
