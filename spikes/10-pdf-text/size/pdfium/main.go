// Size probe: what go-pdfium's WebAssembly build adds to a binary.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
)

func main() {
	pool, err := webassembly.Init(webassembly.Config{MinIdle: 1, MaxIdle: 1, MaxTotal: 1})
	if err != nil {
		panic(err)
	}
	inst, _ := pool.GetInstance(time.Minute)
	data, _ := os.ReadFile(os.Args[1])
	doc, err := inst.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		panic(err)
	}
	t, _ := inst.GetPageText(&requests.GetPageText{Page: requests.Page{ByIndex: &requests.PageByIndex{Document: doc.Document}}})
	fmt.Println(len(t.Text))
}
