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
	"path/filepath"
	"runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/codevector-2003/filiation/internal/assistants"
	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/opener"
	"github.com/codevector-2003/filiation/internal/ui"
)

func main() {
	opts := openLibrary(context.Background())
	if env, err := assistants.DefaultEnv(filCommand()); err == nil {
		opts.Assistants = &env
	}
	if dir, err := config.DefaultCacheDir(); err == nil {
		opts.CallLog = filepath.Join(dir, assistants.CallLogName)
	}
	var appCtx context.Context
	opts.PickFiles = func() ([]string, error) {
		return wruntime.OpenMultipleFilesDialog(appCtx, wruntime.OpenDialogOptions{Title: "Add PDFs",
			Filters: []wruntime.FileFilter{{DisplayName: "PDF files", Pattern: "*.pdf"}}})
	}
	opts.PickExport = func() (string, error) {
		return wruntime.OpenFileDialog(appCtx, wruntime.OpenDialogOptions{Title: "Choose a Zotero RDF export",
			Filters: []wruntime.FileFilter{{DisplayName: "Zotero RDF export", Pattern: "*.rdf"}}})
	}
	err := run(ui.New(opts), func(ctx context.Context) { appCtx = ctx })
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
	// With no terminal to ask in, a first run takes the default place, as an
	// assistant's first fil mcp does.
	if cfg.FirstRun {
		if err := cfg.Save(); err != nil {
			opts.Problem = err.Error()
			return opts
		}
	}
	opts.Name = cfg.Name
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

// filCommand is how an assistant starts fil: the fil beside this app when
// there is one, because an assistant may not have fil on its PATH.
func filCommand() string {
	exe, err := os.Executable()
	if err != nil {
		return "fil"
	}
	name := "fil"
	if runtime.GOOS == "windows" {
		name = "fil.exe"
	}
	beside := filepath.Join(filepath.Dir(exe), name)
	if _, err := os.Stat(beside); err != nil {
		return "fil"
	}
	return beside
}

func run(h http.Handler, started func(context.Context)) error {
	return wails.Run(&options.App{
		Title:            "Filiation",
		Width:            1320,
		Height:           840,
		MinWidth:         960,
		MinHeight:        600,
		BackgroundColour: &options.RGBA{R: 0xF6, G: 0xF5, B: 0xF2, A: 0xFF},
		AssetServer:      &assetserver.Options{Handler: h},
		DragAndDrop:      &options.DragAndDrop{EnableFileDrop: true, DisableWebViewDrop: true},
		OnStartup:        started,
	})
}
