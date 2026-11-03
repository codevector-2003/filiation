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
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/codevector-2003/filiation/internal/assistants"
	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/opener"
	"github.com/codevector-2003/filiation/internal/ui"
)

// version is set at release with -ldflags "-X main.version=…".
var version = "dev"

func main() {
	opts := ui.Options{OpenPath: opener.Open, Version: version}
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
	opts.PickFolder = func(title string) (string, error) {
		return wruntime.OpenDirectoryDialog(appCtx, wruntime.OpenDialogOptions{Title: title, CanCreateDirectories: true})
	}
	win := ui.NewWindow(context.Background(), opts)
	err := run(win, func(ctx context.Context) { appCtx = ctx }, &appCtx)
	win.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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

// run opens the window. On Windows it has no frame: the page draws the title
// bar, with the menus and the window's buttons, as VS Code does. macOS keeps
// its three buttons over the page and puts the menus in the menu bar at the top
// of the screen. Linux keeps the frame its window manager draws, since Wails
// cannot resize a frameless window there, and the page draws the menus below it.
func run(h http.Handler, started func(context.Context), appCtx *context.Context) error {
	app := &options.App{
		Title:            "Filiation",
		Width:            1320,
		Height:           840,
		MinWidth:         960,
		MinHeight:        600,
		Frameless:        runtime.GOOS == "windows",
		BackgroundColour: &options.RGBA{R: 0xF6, G: 0xF5, B: 0xF2, A: 0xFF},
		AssetServer:      &assetserver.Options{Handler: h},
		DragAndDrop:      &options.DragAndDrop{EnableFileDrop: true, DisableWebViewDrop: true},
		OnStartup:        started,
		Mac:              &mac.Options{TitleBar: mac.TitleBarHiddenInset()},
	}
	if runtime.GOOS == "darwin" {
		app.Menu = menuBar(appCtx)
	}
	return wails.Run(app)
}

// menuBar is macOS's menu bar, built from the menus the page draws elsewhere.
// Each item sends its ID to the page, which does what it says. Edit is the
// system's own, so text boxes behave as a Mac user expects.
func menuBar(appCtx *context.Context) *menu.Menu {
	bar := menu.NewMenu()
	bar.Append(menu.AppMenu())
	for _, m := range ui.Menus {
		if m.Name == "Edit" {
			bar.Append(menu.EditMenu())
			continue
		}
		if m.Name == "Help" {
			bar.Append(menu.WindowMenu())
		}
		sub := bar.AddSubmenu(m.Name)
		for _, it := range m.Items {
			switch {
			case it.ID == "":
				sub.AddSeparator()
				continue
			case it.ID == "app.quit": // the app menu has Quit
				continue
			}
			var acc *keys.Accelerator
			// Cmd+M minimises on a Mac, whatever the app says.
			if it.Keys != "" && it.Keys != "cmdorctrl+m" {
				acc, _ = keys.Parse(it.Keys)
			}
			id := it.ID
			sub.AddText(it.Label, acc, func(*menu.CallbackData) { wruntime.EventsEmit(*appCtx, "menu", id) })
		}
	}
	return bar
}
