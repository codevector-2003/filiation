package ui

import "net/http"

// Menu is one menu of the title bar.
type Menu struct {
	Name  string     `json:"name"`
	Items []MenuItem `json:"items"`
}

// MenuItem is one entry in a menu, or a separator when ID is "".
//
// The page draws the menus from these and runs each by its ID. On macOS the
// desktop app builds the system menu bar from the same list, and an item sends
// its ID to the page; Edit there is the system's own, so text boxes behave as
// a Mac user expects.
type MenuItem struct {
	ID    string `json:"id,omitempty"`
	Label string `json:"label,omitempty"`
	// Keys is the shortcut as Wails writes an accelerator, "cmdorctrl+shift+n".
	Keys string `json:"keys,omitempty"`
	// Shown is a shortcut shown but not bound: Alt+F4, which the system
	// handles, and chords such as Ctrl+K Ctrl+S, which a menu bar cannot hold.
	Shown string `json:"shown,omitempty"`
	// Edit marks the text-editing commands: the page leaves their keys to the
	// browser, and the menu asks the browser to do them.
	Edit bool `json:"edit,omitempty"`
	// Sub names a submenu the page fills: "recent", the other libraries.
	Sub string `json:"sub,omitempty"`
}

var sep = MenuItem{}

// Menus is the title bar, in order.
var Menus = []Menu{
	{"File", []MenuItem{
		{ID: "lib.new", Label: "New Library…", Keys: "cmdorctrl+shift+n"},
		{ID: "lib.open", Label: "Open Library Folder…", Keys: "cmdorctrl+o"},
		{ID: "lib.recent", Label: "Open Recent", Sub: "recent"},
		sep,
		{ID: "paper.add", Label: "Add Paper…", Keys: "cmdorctrl+n"},
		{ID: "pdfs.import", Label: "Import PDFs…", Keys: "cmdorctrl+i"},
		{ID: "zotero", Label: "Bring In from Zotero…"},
		sep,
		{ID: "export.graphml", Label: "Export Graph (GraphML)", Keys: "cmdorctrl+e"},
		sep,
		{ID: "lib.reveal", Label: "Reveal Library Folder", Keys: "cmdorctrl+shift+r"},
		{ID: "lib.close", Label: "Close Library"},
		sep,
		{ID: "app.quit", Label: "Exit", Shown: "Alt+F4"},
	}},
	{"Edit", []MenuItem{
		{ID: "edit.undo", Label: "Undo", Keys: "cmdorctrl+z", Edit: true},
		{ID: "edit.redo", Label: "Redo", Keys: "cmdorctrl+y", Edit: true},
		sep,
		{ID: "edit.cut", Label: "Cut", Keys: "cmdorctrl+x", Edit: true},
		{ID: "edit.copy", Label: "Copy", Keys: "cmdorctrl+c", Edit: true},
		{ID: "edit.paste", Label: "Paste", Keys: "cmdorctrl+v", Edit: true},
		{ID: "edit.selectAll", Label: "Select All", Keys: "cmdorctrl+a", Edit: true},
		sep,
		{ID: "paper.cite", Label: "Copy Citation", Keys: "cmdorctrl+shift+c"},
		{ID: "paper.copyId", Label: "Copy fil ID"},
		sep,
		{ID: "find.library", Label: "Find in Library", Keys: "cmdorctrl+f"},
		{ID: "find.text", Label: "Search Full Text", Keys: "cmdorctrl+shift+f"},
	}},
	{"View", []MenuItem{
		{ID: "view.papers", Label: "Papers", Keys: "cmdorctrl+1"},
		{ID: "view.favourites", Label: "Favourites", Keys: "cmdorctrl+2"},
		{ID: "view.search", Label: "Search", Keys: "cmdorctrl+3"},
		{ID: "view.map", Label: "Citation Map", Keys: "cmdorctrl+4"},
		{ID: "view.inbox", Label: "Inbox", Keys: "cmdorctrl+5"},
		{ID: "view.assistants", Label: "AI Assistants", Keys: "cmdorctrl+6"},
		{ID: "view.groups", Label: "Suggested Groups", Keys: "cmdorctrl+7"},
		sep,
		{ID: "view.sidebar", Label: "Sidebar", Keys: "cmdorctrl+b"},
		{ID: "view.details", Label: "Paper Details", Keys: "cmdorctrl+j"},
		{ID: "view.mapFilters", Label: "Citation Map Filters", Keys: "cmdorctrl+shift+m"},
		sep,
		{ID: "view.zoomIn", Label: "Zoom In", Keys: "cmdorctrl+="},
		{ID: "view.zoomOut", Label: "Zoom Out", Keys: "cmdorctrl+-"},
		{ID: "view.zoomReset", Label: "Reset Zoom", Keys: "cmdorctrl+0"},
		{ID: "view.fullscreen", Label: "Full Screen", Keys: "f11"},
	}},
	{"Library", []MenuItem{
		{ID: "graph.expand", Label: "Expand Citation Graph…"},
		{ID: "pdfs.fetch", Label: "Fetch Open-Access PDFs"},
		{ID: "job.stop", Label: "Stop Current Job"},
		sep,
		{ID: "collection.new", Label: "New Collection…"},
		{ID: "label.new", Label: "New Label…"},
		sep,
		{ID: "lib.scan", Label: "Rescan Papers Folder"},
		{ID: "lib.stats", Label: "Library Statistics"},
		sep,
		{ID: "lib.move", Label: "Move Library…"},
	}},
	{"Go", []MenuItem{
		{ID: "go.back", Label: "Back", Keys: "optionoralt+left"},
		{ID: "go.forward", Label: "Forward", Keys: "optionoralt+right"},
		sep,
		{ID: "go.paper", Label: "Go to Paper…", Keys: "cmdorctrl+p"},
		{ID: "go.path", Label: "Path Between Papers…"},
	}},
	{"Paper", []MenuItem{
		{ID: "paper.read", Label: "Read"},
		{ID: "paper.open", Label: "Open PDF"},
		{ID: "paper.download", Label: "Download PDF", Keys: "cmdorctrl+d"},
		{ID: "paper.attach", Label: "Add Your PDF…"},
		{ID: "paper.around", Label: "Bring In Papers Around This…", Keys: "cmdorctrl+shift+e"},
		{ID: "paper.map", Label: "Show on Citation Map", Keys: "cmdorctrl+m"},
		sep,
		{ID: "paper.favourite", Label: "Favourite"},
		{ID: "paper.collect", Label: "Add to Collection…"},
		{ID: "paper.label", Label: "Label…"},
		sep,
		{ID: "paper.folder", Label: "Show in Folder"},
		{ID: "paper.cite", Label: "Copy Citation", Keys: "cmdorctrl+shift+c"},
		{ID: "paper.copyId", Label: "Copy fil ID"},
	}},
	{"Help", []MenuItem{
		{ID: "help.docs", Label: "Documentation", Keys: "f1"},
		{ID: "help.keys", Label: "Keyboard Shortcuts", Shown: "Ctrl+K Ctrl+S"},
		{ID: "help.assistant", Label: "Connect an AI Assistant"},
		sep,
		{ID: "help.issue", Label: "Report an Issue"},
		{ID: "help.licence", Label: "View Licence"},
		sep,
		{ID: "help.about", Label: "About Filiation"},
	}},
}

func (s *server) menus(w http.ResponseWriter, _ *http.Request) {
	send(w, map[string]any{"menus": Menus, "version": s.Version})
}
