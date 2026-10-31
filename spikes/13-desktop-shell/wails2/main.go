// Spike 13, Wails v2: a native window whose page is served by a plain Go
// http.Handler in the same process — how fil's interface would be shown.
// Its own module, so Wails never enters fil's go.mod (D24).
//
//	go build -tags desktop,production .            (Windows, macOS)
//	go build -tags desktop,production,webkit2_41 . (Linux: WebKitGTK 4.1)
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func main() {
	start := time.Now()
	var appCtx context.Context
	h := newHandler(start, "wails2", func() { wruntime.Quit(appCtx) })
	err := wails.Run(&options.App{
		Title:       "Filiation spike (Wails v2)",
		Width:       900,
		Height:      600,
		AssetServer: &assetserver.Options{Handler: h},
		OnStartup:   func(ctx context.Context) { appCtx = ctx },
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
