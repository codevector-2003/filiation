package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/codevector-2003/filiation/internal/errs"
)

// Defaults. Every one of these is a number the user may override; none of them
// is a limit of the tool.
const (
	// DefaultMaxNodes is the primary control on how large a graph may grow.
	// 500 is large enough to be a map of a literature and small enough to
	// finish in well under a minute, and it costs 5 credits of a 1,000/day
	// allowance (D11). Depth is only a secondary guard: at the measured 70-100
	// references per STEM paper, depth 2 alone is ~5,000 nodes, so this number
	// is what actually stops an expansion.
	DefaultMaxNodes = 500

	// DefaultMaxDepth stops a run that would otherwise wander a long way from
	// the seed before the node budget bites. It is rarely the binding limit.
	DefaultMaxDepth = 3

	// DefaultMaxRefsPerWork caps how many candidates one work may contribute to
	// the frontier. A review article citing 800 papers would otherwise consume
	// an entire budget by itself. It does not cap edges: every reference is
	// recorded, because they arrive free inside the response and throwing them
	// away would falsify the graph.
	DefaultMaxRefsPerWork = 100

	// FileName is the settings file: the global one in the per-user directory
	// (ADR-006), and each library folder's own (ADR-011).
	FileName = "filiation.toml"

	// DBName is the database of a v0.1 or v0.2 library, which is a file with
	// no folder around it.
	DBName = "library.db"

	// LibraryDBName is the database inside a library folder.
	LibraryDBName = "filiation.db"

	// DefaultLibraryName is the first library's name, and its folder's.
	DefaultLibraryName = "My Library"
)

// Config is the resolved configuration: what the tool will actually do, after
// the flags, the environment, the files and the defaults have been reconciled.
type Config struct {
	// DBPath is absolute. Everything downstream may open it without further
	// cleaning.
	DBPath string

	// Folder is the library folder (ADR-011), absolute: papers/, Inbox/ and
	// exports/ live in it. Empty for a library that is only a database — one
	// made by v0.1 or v0.2, or named with --db — until fil library move gives
	// it a folder.
	Folder string

	// Name is the library's name: what --library takes, and what the global
	// settings list it under. Empty for a library that is only a database.
	Name string

	// Cloud names the sync service the folder is in, or is empty. When it is
	// set, the database is kept outside the folder (CloudService).
	Cloud string

	// ContactEmail is sent to OpenAlex as mailto. It is optional: the spikes
	// measured byte-identical rate-limit headers with and without it (D11), so
	// requiring it would punish the user for a politeness that buys them
	// nothing. Absent, Warnings says so once and the tool runs.
	ContactEmail string

	// Theme is the colour scheme fil uses in a terminal: a name the CLI knows,
	// or empty for its default. ThemeBackground is "dark", "light", or empty
	// to guess from the terminal. Neither changes anything but colours.
	Theme           string
	ThemeBackground string

	MaxNodes       int
	MaxDepth       int
	MaxRefsPerWork int

	// FolderCollections makes each folder in papers/ a collection (ADR-012).
	// On by default; a library's own settings file can turn it off.
	FolderCollections bool

	// ConfigPath is the global settings file, whether or not it existed. The
	// CLI needs it to tell the user which file to edit.
	ConfigPath string

	// LibraryConfigPath is the library folder's own settings file, or empty
	// for a library with no folder.
	LibraryConfigPath string

	// FirstRun reports that no library has been chosen yet. The CLI uses it to
	// offer a location and to print where the library lives — without that,
	// users cannot find their own data, because the default differs on every
	// OS (ADR-006).
	FirstRun bool

	// Libraries are the library folders the global settings list.
	Libraries []Library

	global  globalFile
	appDir  string
	libFile libraryFile
}

// Library is one library folder the global settings list.
type Library struct {
	Name   string `toml:"name"`
	Folder string `toml:"folder"`
	// Database is where the database is kept when not in the folder — set for
	// a folder in a cloud sync service. Empty means Folder/filiation.db.
	Database string `toml:"database,omitempty"`
}

// Options are the inputs to Load that do not come from a settings file.
type Options struct {
	// DBFlag is the value of --db, empty when the flag was not passed. It wins
	// over every other source, and names a database with no folder.
	DBFlag string

	// LibraryFlag is the value of --library: a library's name, or its folder.
	LibraryFlag string

	// Dir is the directory holding the global settings file. Empty means the
	// per-user default from DefaultDir. Tests set it so they never read or
	// write the real one.
	Dir string

	// DocumentsDir replaces the user's Documents folder, under which the first
	// library is proposed. Tests set it so they never write to the real one.
	DocumentsDir string
}

// globalFile is the on-disk form of the global settings. Every scalar is a
// pointer so that absent and zero stay distinguishable: a file that does not
// mention max_nodes must leave the default alone, while max_nodes = 0 is a
// mistake worth reporting.
type globalFile struct {
	ContactEmail    *string `toml:"contact_email,omitempty"`
	DefaultLibrary  *string `toml:"default_library,omitempty"`
	Theme           *string `toml:"theme,omitempty"`
	ThemeBackground *string `toml:"theme_background,omitempty"`

	// DBPath is a v0.1 or v0.2 library: a database with no folder. While it is
	// set and default_library is not, it is the library fil opens.
	DBPath *string `toml:"db_path,omitempty"`

	// Budgets here apply to every library, under its own settings.
	MaxNodes       *int `toml:"max_nodes,omitempty"`
	MaxDepth       *int `toml:"max_depth,omitempty"`
	MaxRefsPerWork *int `toml:"max_refs_per_work,omitempty"`

	Libraries []Library `toml:"library,omitempty"`
}

// libraryFile is the on-disk form of a library folder's own settings. It is
// read from the folder, so it travels with the library.
type libraryFile struct {
	Name              *string `toml:"name,omitempty"`
	MaxNodes          *int    `toml:"max_nodes,omitempty"`
	MaxDepth          *int    `toml:"max_depth,omitempty"`
	MaxRefsPerWork    *int    `toml:"max_refs_per_work,omitempty"`
	FolderCollections *bool   `toml:"folder_collections,omitempty"`
}

// DefaultDir is the per-user directory holding the global settings and, for a
// library in a cloud sync folder, its database.
//
// os.UserConfigDir is stdlib and already correct on all three targets —
// %AppData% on Windows, ~/Library/Application Support on macOS, and
// $XDG_CONFIG_HOME or ~/.config on Linux — so the adrg/xdg dependency suggested
// by ADR-006 is not taken.
func DefaultDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(base, "filiation"), nil
}

// DefaultCacheDir is where the HTTP response cache lives unless told otherwise.
//
// It is deliberately not beside the library. The cache is regenerable and the
// library is not, so it goes where each OS expects disposable data —
// %LocalAppData% on Windows, ~/Library/Caches on macOS, $XDG_CACHE_HOME or
// ~/.cache on Linux — where cleanup tools and backup exclusions already treat
// it as such.
func DefaultCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate user cache directory: %w", err)
	}
	return filepath.Join(base, "filiation", "http"), nil
}

// DefaultCompiledDir is where the PDF extractor keeps its compiled code, so
// it starts in a tenth of a second rather than three. Regenerable, like the
// HTTP cache, so it sits beside it.
func DefaultCompiledDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate user cache directory: %w", err)
	}
	return filepath.Join(base, "filiation", "pdfium"), nil
}

// Load resolves configuration.
//
// Which library, highest priority first: the --db flag, FILIATION_DB,
// --library, the global settings (default_library, then a v0.2 db_path, then
// the first library listed), and on a first run a new folder proposed under
// Documents/Filiation.
//
// Budgets, lowest priority first: the defaults, the global settings, then the
// library folder's own settings. Precedence is per field, not per source.
//
// A missing settings file is the normal case on a first run and is not an
// error. A malformed one is: falling back to defaults would let a typo in
// max_nodes silently expand a graph nobody asked for.
func Load(opts Options) (*Config, error) {
	dir := opts.Dir
	if dir == "" {
		d, err := DefaultDir()
		if err != nil {
			return nil, err
		}
		dir = d
	}
	cfg := &Config{
		MaxNodes:          DefaultMaxNodes,
		MaxDepth:          DefaultMaxDepth,
		MaxRefsPerWork:    DefaultMaxRefsPerWork,
		FolderCollections: true,
		ConfigPath:        filepath.Join(dir, FileName),
		appDir:            dir,
	}

	var g globalFile
	_, err := readTOML(cfg.ConfigPath, &g)
	if err != nil {
		return nil, err
	}
	cfg.global = g
	cfg.Libraries = append(cfg.Libraries, g.Libraries...)
	if g.ContactEmail != nil {
		cfg.ContactEmail = *g.ContactEmail
	}
	if g.Theme != nil {
		cfg.Theme = *g.Theme
	}
	if g.ThemeBackground != nil {
		cfg.ThemeBackground = *g.ThemeBackground
	}
	applyBudgets(cfg, g.MaxNodes, g.MaxDepth, g.MaxRefsPerWork)

	switch {
	case strings.TrimSpace(opts.DBFlag) != "":
		cfg.DBPath = strings.TrimSpace(opts.DBFlag)
	case strings.TrimSpace(os.Getenv(EnvDB)) != "":
		cfg.DBPath = strings.TrimSpace(os.Getenv(EnvDB))
	case strings.TrimSpace(opts.LibraryFlag) != "":
		lib, err := cfg.find(strings.TrimSpace(opts.LibraryFlag))
		if err != nil {
			return nil, err
		}
		cfg.use(lib)
	case g.DefaultLibrary != nil:
		lib, ok := cfg.byName(*g.DefaultLibrary)
		if !ok {
			return nil, fmt.Errorf("default_library %q in %s is not one of the libraries listed there: %w",
				*g.DefaultLibrary, cfg.ConfigPath, errs.ErrInvalidConfig)
		}
		cfg.use(lib)
	case g.DBPath != nil:
		cfg.DBPath = *g.DBPath
	case len(cfg.Libraries) > 0:
		cfg.use(cfg.Libraries[0])
	default:
		docs := opts.DocumentsDir
		if docs == "" {
			if docs, err = DocumentsDir(); err != nil {
				return nil, fmt.Errorf("locate Documents folder: %w", err)
			}
		}
		// Whether or not a settings file exists, no library is recorded in
		// it: this is a proposal for the front door to confirm and Save.
		cfg.FirstRun = true
		if err := cfg.UseFolder(filepath.Join(docs, "Filiation", DefaultLibraryName)); err != nil {
			return nil, err
		}
	}

	if err := cfg.normalise(); err != nil {
		return nil, err
	}
	if cfg.Folder != "" {
		if err := cfg.readLibraryFile(); err != nil {
			return nil, err
		}
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// EnvDB overrides the library path from the environment. It sits between the
// --db flag and everything else.
const EnvDB = "FILIATION_DB"

// UseFolder points this configuration at the library folder dir, which need
// not exist yet. The database goes in the folder, unless the folder is inside
// a cloud sync service: then it goes in the per-user directory, under a name
// that the folder's path makes unique.
func (c *Config) UseFolder(dir string) error {
	abs, err := absPath(dir)
	if err != nil {
		return err
	}
	c.Folder = abs
	c.Name = filepath.Base(abs)
	c.DBPath = c.databaseFor(abs)
	c.Cloud = CloudService(abs)
	c.LibraryConfigPath = filepath.Join(abs, FileName)
	return nil
}

// databaseFor is where a library folder's database belongs.
func (c *Config) databaseFor(folder string) string {
	if CloudService(folder) == "" {
		return filepath.Join(folder, LibraryDBName)
	}
	key := folder
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		key = strings.ToLower(key)
	}
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.appDir, "libraries", slug(filepath.Base(folder))+"-"+hex.EncodeToString(sum[:4]), LibraryDBName)
}

// slug keeps a name readable as a directory name and drops the rest.
func slug(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "library"
	}
	return b.String()
}

// use points this configuration at a listed library.
func (c *Config) use(lib Library) {
	c.Folder = lib.Folder
	c.Name = lib.Name
	c.DBPath = lib.Database
	if c.DBPath == "" {
		c.DBPath = filepath.Join(lib.Folder, LibraryDBName)
	}
	c.Cloud = CloudService(lib.Folder)
	c.LibraryConfigPath = filepath.Join(lib.Folder, FileName)
}

// find resolves --library: a listed library's name, or a folder holding a
// library's settings file.
func (c *Config) find(nameOrFolder string) (Library, error) {
	if lib, ok := c.byName(nameOrFolder); ok {
		return lib, nil
	}
	abs, err := absPath(nameOrFolder)
	if err != nil {
		return Library{}, err
	}
	for _, lib := range c.Libraries {
		if samePath(lib.Folder, abs) {
			return lib, nil
		}
	}
	if _, err := os.Stat(filepath.Join(abs, FileName)); err == nil {
		return Library{Name: filepath.Base(abs), Folder: abs, Database: c.unlessInFolder(abs, c.databaseFor(abs))}, nil
	}
	var names []string
	for _, lib := range c.Libraries {
		names = append(names, lib.Name)
	}
	known := "none are listed yet"
	if len(names) > 0 {
		known = "listed: " + strings.Join(names, ", ")
	}
	return Library{}, fmt.Errorf("no library %q — not a name in %s (%s), nor a library folder: %w",
		nameOrFolder, c.ConfigPath, known, errs.ErrInvalidConfig)
}

func (c *Config) byName(name string) (Library, bool) {
	for _, lib := range c.Libraries {
		if strings.EqualFold(lib.Name, name) {
			return lib, true
		}
	}
	return Library{}, false
}

// unlessInFolder returns db, or "" when db is the folder's own filiation.db,
// which is what a listed library's empty database field means.
func (c *Config) unlessInFolder(folder, db string) string {
	if samePath(db, filepath.Join(folder, LibraryDBName)) {
		return ""
	}
	return db
}

// readLibraryFile reads the library folder's settings. A folder not created
// yet has none, which is ordinary.
func (c *Config) readLibraryFile() error {
	var f libraryFile
	if _, err := readTOML(c.LibraryConfigPath, &f); err != nil {
		return err
	}
	c.libFile = f
	if f.Name != nil && strings.TrimSpace(*f.Name) != "" && c.Name == filepath.Base(c.Folder) {
		c.Name = strings.TrimSpace(*f.Name)
	}
	applyBudgets(c, f.MaxNodes, f.MaxDepth, f.MaxRefsPerWork)
	if f.FolderCollections != nil {
		c.FolderCollections = *f.FolderCollections
	}
	return nil
}

func applyBudgets(c *Config, nodes, depth, refs *int) {
	if nodes != nil {
		c.MaxNodes = *nodes
	}
	if depth != nil {
		c.MaxDepth = *depth
	}
	if refs != nil {
		c.MaxRefsPerWork = *refs
	}
}

// readTOML decodes the file at path into v if the file is there. Absent is
// reported as not found rather than as an error, because absent is the
// ordinary first run.
func readTOML(path string, v any) (bool, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	md, err := toml.Decode(string(b), v)
	if err != nil {
		return false, fmt.Errorf("parse %s: %v: %w", path, err, errs.ErrInvalidConfig)
	}
	// A key TOML parsed but the struct has no field for is almost always a
	// typo, and silence is the worst answer to it: max_node = 5000 would parse
	// cleanly, change nothing, and leave the user believing they had raised a
	// budget they had not. Refusing costs one clear message; accepting costs an
	// expansion nobody asked for.
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, 0, len(undecoded))
		for _, k := range undecoded {
			keys = append(keys, k.String())
		}
		return false, fmt.Errorf("unknown setting %s in %s: %w",
			strings.Join(keys, ", "), path, errs.ErrInvalidConfig)
	}
	return true, nil
}

// absPath expands a leading ~ and makes p absolute. The tilde is handled
// because paths are hand-edited by people who expect it to work, and Go does
// no shell expansion of its own.
func absPath(p string) (string, error) {
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand ~ in %q: %w", p, err)
		}
		p = filepath.Join(home, p[2:])
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %v: %w", p, err, errs.ErrInvalidConfig)
	}
	return abs, nil
}

// samePath compares two absolute paths, ignoring case where the file system
// does.
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func (c *Config) normalise() error {
	if c.DBPath == "" {
		return nil
	}
	abs, err := absPath(c.DBPath)
	if err != nil {
		return err
	}
	c.DBPath = abs
	return nil
}

// validate catches what is wrong here rather than on the fortieth API call.
func (c *Config) validate() error {
	if c.DBPath == "" {
		return fmt.Errorf("library path is empty: %w", errs.ErrInvalidConfig)
	}
	if c.MaxNodes < 1 {
		return fmt.Errorf("max_nodes is %d, must be at least 1: %w", c.MaxNodes, errs.ErrInvalidConfig)
	}
	if c.MaxDepth < 1 {
		return fmt.Errorf("max_depth is %d, must be at least 1: %w", c.MaxDepth, errs.ErrInvalidConfig)
	}
	if c.MaxRefsPerWork < 1 {
		return fmt.Errorf("max_refs_per_work is %d, must be at least 1: %w", c.MaxRefsPerWork, errs.ErrInvalidConfig)
	}
	if e := c.ContactEmail; e != "" && (!strings.Contains(e, "@") || strings.ContainsAny(e, " \t")) {
		return fmt.Errorf("contact_email %q is not an address: %w", e, errs.ErrInvalidConfig)
	}
	for i, lib := range c.Libraries {
		if strings.TrimSpace(lib.Name) == "" || strings.TrimSpace(lib.Folder) == "" {
			return fmt.Errorf("library %d in %s needs both a name and a folder: %w", i+1, c.ConfigPath, errs.ErrInvalidConfig)
		}
		for _, other := range c.Libraries[:i] {
			if strings.EqualFold(other.Name, lib.Name) {
				return fmt.Errorf("two libraries are named %q in %s: %w", lib.Name, c.ConfigPath, errs.ErrInvalidConfig)
			}
		}
	}
	return nil
}

// Warnings is what the CLI should tell the user without refusing to run.
//
// It returns strings rather than printing, because config must stay usable from
// a test, an MCP server and a web handler, none of which have a terminal to
// print to.
func (c *Config) Warnings() []string {
	var w []string
	if c.ContactEmail == "" {
		w = append(w, "no contact email set: OpenAlex asks for one so they can reach you. Set contact_email in "+c.ConfigPath)
	}
	return w
}

// MakeDefault makes this library the one fil opens when not told otherwise.
func (c *Config) MakeDefault() {
	if c.Folder == "" {
		c.global.DefaultLibrary = nil
		c.global.DBPath = &c.DBPath
		return
	}
	name := c.Name
	c.global.DefaultLibrary = &name
}

// DropDatabaseOnly forgets the v0.2 library named by db_path. fil library move
// uses it once the library has a folder; the old file stays on disk.
func (c *Config) DropDatabaseOnly() { c.global.DBPath = nil }

// IsRecordedDatabase reports whether path is the v0.2 library db_path names.
func (c *Config) IsRecordedDatabase(path string) bool {
	return c.global.DBPath != nil && samePath(*c.global.DBPath, path)
}

// Forget removes the library at folder from the global list, for a library
// that is about to be listed again at a new folder. Nothing on disk changes.
func (c *Config) Forget(folder string) {
	kept := c.global.Libraries[:0:0]
	for _, lib := range c.global.Libraries {
		if !samePath(lib.Folder, folder) {
			kept = append(kept, lib)
		}
	}
	c.global.Libraries = kept
}

// Save writes the settings, creating directories as needed.
//
// It is how a location chosen on first run survives to the second. Choosing is
// not this package's job: config never prompts, because it has to work where
// there is no terminal. The front door asks; this records the answer.
//
// For a library folder it lists the library in the global settings — as the
// default, on a first run — and writes the folder's own settings file if there
// is none yet. For a library that is only a database it records db_path and
// the budgets in the global settings, as v0.2 did.
func (c *Config) Save() error {
	if c.ConfigPath == "" {
		return fmt.Errorf("no config path set: %w", errs.ErrInvalidConfig)
	}
	g := c.global
	if c.ContactEmail != "" || g.ContactEmail != nil {
		email := c.ContactEmail
		g.ContactEmail = &email
	}

	if c.Folder == "" {
		db := c.DBPath
		g.DBPath = &db
		nodes, depth, refs := c.MaxNodes, c.MaxDepth, c.MaxRefsPerWork
		g.MaxNodes, g.MaxDepth, g.MaxRefsPerWork = &nodes, &depth, &refs
	} else {
		entry := Library{Name: c.Name, Folder: c.Folder, Database: c.unlessInFolder(c.Folder, c.DBPath)}
		replaced := false
		for i, lib := range g.Libraries {
			if samePath(lib.Folder, c.Folder) {
				g.Libraries[i], replaced = entry, true
			} else if strings.EqualFold(lib.Name, c.Name) {
				return fmt.Errorf("a library named %q is already at %s: %w", c.Name, lib.Folder, errs.ErrInvalidConfig)
			}
		}
		if !replaced {
			g.Libraries = append(g.Libraries, entry)
		}
		if c.FirstRun || (g.DefaultLibrary == nil && g.DBPath == nil && len(g.Libraries) == 1) {
			name := c.Name
			g.DefaultLibrary = &name
		}
		if err := c.saveLibraryFile(); err != nil {
			return err
		}
	}

	if err := writeTOML(c.ConfigPath, globalHeader, g); err != nil {
		return err
	}
	c.global = g
	c.Libraries = append(c.Libraries[:0], g.Libraries...)
	c.FirstRun = false
	return nil
}

// globalHeader opens the global settings file.
const globalHeader = "# Filiation settings for this computer.\n" +
	"# Written by fil; edit freely. Delete a line to return it to its default.\n" +
	"# Each library folder has its own filiation.toml for its name and budgets.\n\n"

// SaveTheme records the colour scheme, and the terminal background when
// background is not "", in the global settings, and nothing else: no
// library is created or listed, so it works before the first one exists.
func (c *Config) SaveTheme(name, background string) error {
	if c.ConfigPath == "" {
		return fmt.Errorf("no config path set: %w", errs.ErrInvalidConfig)
	}
	g := c.global
	g.Theme = &name
	if background != "" {
		g.ThemeBackground = &background
	}
	if err := writeTOML(c.ConfigPath, globalHeader, g); err != nil {
		return err
	}
	c.global = g
	c.Theme = name
	if background != "" {
		c.ThemeBackground = background
	}
	return nil
}

// saveLibraryFile writes the library folder's settings unless it already has
// a file, which a person may have edited and which this never overwrites.
func (c *Config) saveLibraryFile() error {
	if _, err := os.Stat(c.LibraryConfigPath); err == nil {
		return nil
	}
	name := c.Name
	nodes, depth, refs, folders := c.MaxNodes, c.MaxDepth, c.MaxRefsPerWork, c.FolderCollections
	f := libraryFile{Name: &name, MaxNodes: &nodes, MaxDepth: &depth, MaxRefsPerWork: &refs, FolderCollections: &folders}
	header := "# Settings for this library. They travel with the folder.\n" +
		"# folder_collections: each folder you make in papers/ appears in fil as a collection.\n\n"
	return writeTOML(c.LibraryConfigPath, header, f)
}

func writeTOML(path, header string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	var b strings.Builder
	b.WriteString(header)
	if err := toml.NewEncoder(&b).Encode(v); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
