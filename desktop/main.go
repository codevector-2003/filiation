// The Filiation desktop app: a native window showing the interface in
// internal/ui, in the same process, with no port open (D24, D25). It holds
// nothing of its own. A module of its own, the one place cgo is allowed, so
// Wails never enters fil's go.mod.
//
//	go build -tags desktop,production -ldflags "-H windowsgui" .  (Windows)
//	go build -tags desktop,production .                           (macOS)
//	go build -tags desktop,production,webkit2_41 .                (Linux)
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/opener"
	"github.com/codevector-2003/filiation/internal/ui"
)

func main() {
	opts := openLibrary(context.Background())
	err := run(ui.New(opts))
	if opts.Library != nil {
		opts.Library.Close()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// openLibrary opens the library fil would use. Anything wrong is shown in the
// window, not lost on a console a double-clicked app does not have.
func openLibrary(ctx context.Context) ui.Options {
	opts := ui.Options{OpenPath: opener.Open}
	cfg, err := config.Load(config.Options{})
	if err != nil {
		opts.Problem = err.Error()
		return opts
	}
	opts.Name = cfg.Name
	if cfg.FirstRun {
		return opts
	}
	lib, err := library.Open(ctx, cfg, library.Options{})
	if err != nil {
		opts.Problem = err.Error()
		return opts
	}
	// People move and delete files in papers/ themselves. A scan that fails
	// costs only fresh file records, so the library still opens.
	if lib.Folder() != "" {
		lib.Scan(ctx)
	}
	opts.Library = lib
	return opts
}

func run(h http.Handler) error {
	return wails.Run(&options.App{
		Title:            "Filiation",
		Width:            1320,
		Height:           840,
		MinWidth:         960,
		MinHeight:        600,
		BackgroundColour: &options.RGBA{R: 0xF6, G: 0xF5, B: 0xF2, A: 0xFF},
		AssetServer:      &assetserver.Options{Handler: h},
	})
}
