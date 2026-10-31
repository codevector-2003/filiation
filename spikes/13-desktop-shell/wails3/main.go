// Spike 13, Wails v3: the same shell as wails2/, on Wails v3.
// Its own module, so Wails never enters fil's go.mod (D24).
//
//	go build .
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func main() {
	start := time.Now()
	var app *application.App
	h := newHandler(start, "wails3", func() { app.Quit() })
	app = application.New(application.Options{
		Name:   "Filiation spike (Wails v3)",
		Assets: application.AssetOptions{Handler: h},
	})
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Filiation spike (Wails v3)",
		Width:  900,
		Height: 600,
		URL:    "/",
	})
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
