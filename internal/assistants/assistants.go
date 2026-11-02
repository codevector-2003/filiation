// Package assistants connects AI assistants on this computer to fil mcp: it
// finds them, adds or removes one entry in their settings, and keeps the local
// record of what they asked. Nothing here touches the network.
//
// Another program owns each settings file, so a change is careful: every
// other setting stays, the old file is kept as a backup, the new one replaces
// it in one step, and a file fil cannot read exactly is left alone.
package assistants

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// ServerName is the entry fil adds to an assistant's settings.
const ServerName = "filiation"

// readOnlyFlag is the argument that keeps an assistant from growing the library.
const readOnlyFlag = "--read-only"

// BackupSuffix ends the name of the copy kept of a settings file before a change.
const BackupSuffix = ".before-filiation"

// Env is where to look, and what to run. Tests point it at a folder.
type Env struct {
	Home      string // the user's home folder
	ConfigDir string // os.UserConfigDir
	Command   string // how an assistant starts fil

	// Run runs a command and returns its output; nil means the real thing.
	// LookPath finds a program; nil means exec.LookPath.
	Run      func(name string, args ...string) (string, error)
	LookPath func(name string) (string, error)
}

// DefaultEnv is this computer, with fil started as command.
func DefaultEnv(command string) (Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Env{}, err
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return Env{}, err
	}
	return Env{Home: home, ConfigDir: dir, Command: command}, nil
}

func (e Env) run(name string, args ...string) (string, error) {
	if e.Run != nil {
		return e.Run(name, args...)
	}
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

func (e Env) lookPath(name string) (string, error) {
	if e.LookPath != nil {
		return e.LookPath(name)
	}
	return exec.LookPath(name)
}

// Status is one assistant, as this computer has it.
type Status struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Connected bool   `json:"connected"`
	ReadOnly  bool   `json:"readOnly"`
	Detail    string `json:"detail"`
	File      string `json:"file,omitempty"`  // the settings file changed
	Entry     string `json:"entry,omitempty"` // the entry that goes in it
	Restart   string `json:"restart,omitempty"`
	Problem   string `json:"problem,omitempty"` // its settings could not be read
}

// client is an assistant whose settings are one JSON file.
type client struct {
	id, name string
	section  string // the object holding its servers
	stdio    bool   // its entries name their transport
	file     func(Env) string
	marker   func(Env) string // a folder that exists when it is installed
	restart  string
}

var clients = []client{
	{id: "claude-desktop", name: "Claude Desktop", section: "mcpServers",
		file:    func(e Env) string { return filepath.Join(claudeDesktopDir(e), "claude_desktop_config.json") },
		marker:  claudeDesktopDir,
		restart: "Restart Claude Desktop afterwards to load it."},
	{id: "cursor", name: "Cursor", section: "mcpServers",
		file:    func(e Env) string { return filepath.Join(e.Home, ".cursor", "mcp.json") },
		marker:  func(e Env) string { return filepath.Join(e.Home, ".cursor") },
		restart: "Cursor picks it up after a restart."},
	{id: "vscode", name: "GitHub Copilot", section: "servers", stdio: true,
		file:    func(e Env) string { return filepath.Join(e.ConfigDir, "Code", "User", "mcp.json") },
		marker:  func(e Env) string { return filepath.Join(e.ConfigDir, "Code") },
		restart: "Reload VS Code afterwards to load it."},
	{id: "gemini", name: "Gemini CLI", section: "mcpServers",
		file:    func(e Env) string { return filepath.Join(e.Home, ".gemini", "settings.json") },
		marker:  func(e Env) string { return filepath.Join(e.Home, ".gemini") },
		restart: "It loads the next time Gemini CLI starts."},
}

const claudeCodeID = "claude-code"

// claudeDesktopDir is where Claude Desktop keeps its settings. The Microsoft
// Store version reads them from its own package, never from the usual place.
func claudeDesktopDir(e Env) string {
	store, _ := filepath.Glob(filepath.Join(e.Home, "AppData", "Local", "Packages", "Claude_*", "LocalCache", "Roaming", "Claude"))
	if len(store) > 0 {
		return store[0]
	}
	return filepath.Join(e.ConfigDir, "Claude")
}

func (c client) args(readOnly bool) []string {
	if readOnly {
		return []string{"mcp", readOnlyFlag}
	}
	return []string{"mcp"}
}

func (c client) entry(e Env, readOnly bool) map[string]any {
	out := map[string]any{"command": e.Command, "args": c.args(readOnly)}
	if c.stdio {
		out["type"] = "stdio"
	}
	return out
}

// List reports every assistant fil can set up.
func List(e Env) []Status {
	out := []Status{claudeCode(e)}
	for _, c := range clients {
		out = append(out, c.status(e))
	}
	return out
}

func (c client) status(e Env) Status {
	s := Status{ID: c.id, Name: c.name, File: c.file(e), Restart: c.restart}
	b, _ := json.Marshal(c.entry(e, false))
	s.Entry = fmt.Sprintf("%q: %s", ServerName, b)
	if fi, err := os.Stat(c.marker(e)); err == nil && fi.IsDir() {
		s.Installed = true
	}
	doc, err := readSettings(s.File)
	if err != nil {
		s.Problem = err.Error()
	} else if entry, ok := serverEntry(doc, c.section); ok {
		s.Connected, s.ReadOnly = true, hasReadOnly(entry)
	}
	switch {
	case s.Problem != "":
		s.Detail = "Its settings could not be read"
	case s.Connected:
		s.Detail = "Connected"
	case s.Installed:
		s.Detail = "Installed, not connected"
	default:
		s.Detail = "Not found on this computer"
	}
	return s
}

func claudeCode(e Env) Status {
	s := Status{ID: claudeCodeID, Name: "Claude Code", Restart: "New Claude Code sessions will have it."}
	s.Entry = fmt.Sprintf("claude mcp add --scope user %s -- %s mcp", ServerName, e.Command)
	if _, err := e.lookPath("claude"); err == nil {
		s.Installed = true
	}
	// Its settings file also holds its history, so it is read here and only
	// its own command changes it.
	path := filepath.Join(e.Home, ".claude.json")
	doc, err := readSettings(path)
	if err != nil {
		s.Problem = err.Error()
	} else if entry, ok := serverEntry(doc, "mcpServers"); ok {
		s.Connected, s.ReadOnly = true, hasReadOnly(entry)
	}
	switch {
	case s.Problem != "":
		s.Detail = "Its settings could not be read"
	case s.Connected:
		s.Detail = "Connected"
	case s.Installed:
		s.Detail = "Installed, not connected"
	default:
		s.Detail = "Not found on this computer"
	}
	return s
}

func serverEntry(doc map[string]any, section string) (map[string]any, bool) {
	servers, ok := doc[section].(map[string]any)
	if !ok {
		return nil, false
	}
	entry, ok := servers[ServerName].(map[string]any)
	return entry, ok
}

func hasReadOnly(entry map[string]any) bool {
	args, _ := entry["args"].([]any)
	return slices.Contains(args, any(readOnlyFlag))
}

func find(id string) (client, error) {
	for _, c := range clients {
		if c.id == id {
			return c, nil
		}
	}
	return client{}, fmt.Errorf("no assistant called %q", id)
}

// Connect adds fil to an assistant's settings.
func Connect(e Env, id string, readOnly bool) error {
	if id == claudeCodeID {
		if _, err := e.lookPath("claude"); err != nil {
			return errors.New("Claude Code was not found on this computer")
		}
		// add refuses an entry that exists, so a change of permission replaces it.
		e.run("claude", "mcp", "remove", "--scope", "user", ServerName)
		args := []string{"mcp", "add", "--scope", "user", ServerName, "--", e.Command, "mcp"}
		if readOnly {
			args = append(args, readOnlyFlag)
		}
		if out, err := e.run("claude", args...); err != nil {
			return fmt.Errorf("claude mcp add failed: %s", strings.TrimSpace(out))
		}
		return nil
	}
	c, err := find(id)
	if err != nil {
		return err
	}
	return changeServers(c.file(e), c.section, c.entry(e, readOnly))
}

// Disconnect removes fil from an assistant's settings.
func Disconnect(e Env, id string) error {
	if id == claudeCodeID {
		if out, err := e.run("claude", "mcp", "remove", "--scope", "user", ServerName); err != nil {
			return fmt.Errorf("claude mcp remove failed: %s", strings.TrimSpace(out))
		}
		return nil
	}
	c, err := find(id)
	if err != nil {
		return err
	}
	return changeServers(c.file(e), c.section, nil)
}

// readSettings reads a JSON settings file; a missing file is an empty one. A
// file that is not exactly JSON, as one with comments is not, is an error, so
// that nothing in it can be lost by writing it back.
func readSettings(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) || (err == nil && len(bytes.TrimSpace(b)) == 0) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber() // numbers stay exactly as written
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%s is not plain JSON, so fil left it alone: %v", filepath.Base(path), err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) || doc == nil {
		return nil, fmt.Errorf("%s is not plain JSON settings, so fil left it alone", filepath.Base(path))
	}
	return doc, nil
}

// changeServers sets fil's entry in path, or removes it when entry is nil.
func changeServers(path, section string, entry map[string]any) error {
	doc, err := readSettings(path)
	if err != nil {
		return err
	}
	servers, ok := doc[section].(map[string]any)
	if !ok {
		if doc[section] != nil {
			return fmt.Errorf("%s in %s is not what fil expects, so fil left it alone", section, filepath.Base(path))
		}
		if entry == nil {
			return nil
		}
		servers = map[string]any{}
	}
	if entry == nil {
		delete(servers, ServerName)
	} else {
		servers[ServerName] = entry
	}
	doc[section] = servers

	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if old, err := os.ReadFile(path); err == nil {
		if fi, err := os.Stat(path); err == nil {
			mode = fi.Mode().Perm()
		}
		if err := os.WriteFile(path+BackupSuffix, old, mode); err != nil {
			return fmt.Errorf("could not keep a backup, so nothing was changed: %w", err)
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(out.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
