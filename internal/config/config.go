package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

	// FileName is the config file, and DBName the library, both inside the
	// directory chosen by ADR-006.
	FileName = "filiation.toml"
	DBName   = "library.db"
)

// Config is the resolved configuration: what the tool will actually do, after
// the flag, the environment, the file and the defaults have been reconciled.
type Config struct {
	// DBPath is absolute. Everything downstream may open it without further
	// cleaning.
	DBPath string

	// ContactEmail is sent to OpenAlex as mailto. It is optional: the spikes
	// measured byte-identical rate-limit headers with and without it (D11), so
	// requiring it would punish the user for a politeness that buys them
	// nothing. Absent, Warnings says so once and the tool runs.
	ContactEmail string

	MaxNodes       int
	MaxDepth       int
	MaxRefsPerWork int

	// ConfigPath is where the file was looked for, whether or not it existed.
	// The CLI needs it to tell the user which file to edit.
	ConfigPath string

	// FirstRun reports that no config file was found. The CLI uses it to offer
	// a choice of library location and to print where the library lives —
	// without that, users cannot find their own data, because the default
	// differs on every OS (ADR-006).
	FirstRun bool
}

// Options are the inputs to Load that do not come from the config file.
//
// Both fields exist mainly so tests can run without reading or writing the real
// user configuration directory, which ADR-006 names as the cost of defaulting
// to a per-user location.
type Options struct {
	// DBFlag is the value of --db, empty when the flag was not passed. It wins
	// over every other source.
	DBFlag string

	// Dir is the directory holding the config file. Empty means the per-user
	// default from DefaultDir.
	Dir string
}

// fileConfig is the on-disk form. Every field is a pointer so that absent and
// zero stay distinguishable: a file that does not mention max_nodes must leave
// the default alone, while max_nodes = 0 is a mistake worth reporting.
type fileConfig struct {
	DBPath         *string `toml:"db_path"`
	ContactEmail   *string `toml:"contact_email"`
	MaxNodes       *int    `toml:"max_nodes"`
	MaxDepth       *int    `toml:"max_depth"`
	MaxRefsPerWork *int    `toml:"max_refs_per_work"`
}

// DefaultDir is the per-user directory holding the config file and, unless told
// otherwise, the library beside it.
//
// os.UserConfigDir is stdlib and already correct on all three targets —
// %AppData% on Windows, ~/Library/Application Support on macOS, and
// $XDG_CONFIG_HOME or ~/.config on Linux — so the adrg/xdg dependency suggested
// by ADR-006 is not taken. One library per user, not one per directory: a
// database in the working directory produces a scattering of half-built graphs,
// and the product thesis is a library that accumulates over years.
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

// Load resolves configuration from, highest priority first: the --db flag, the
// FILIATION_DB environment variable, the config file, then the defaults
// (ADR-006).
//
// Precedence is applied per field, not per source. A config file that sets only
// max_nodes keeps that value even when --db is also passed.
//
// A missing config file is the normal case on a first run and is not an error.
// A malformed one is: falling back to defaults would let a typo in max_nodes
// silently expand a graph nobody asked for.
func Load(opts Options) (*Config, error) {
	dir := opts.Dir
	if dir == "" {
		d, err := DefaultDir()
		if err != nil {
			return nil, err
		}
		dir = d
	}
	configPath := filepath.Join(dir, FileName)

	cfg := &Config{
		DBPath:         filepath.Join(dir, DBName),
		MaxNodes:       DefaultMaxNodes,
		MaxDepth:       DefaultMaxDepth,
		MaxRefsPerWork: DefaultMaxRefsPerWork,
		ConfigPath:     configPath,
	}

	// Layer 3: the config file.
	f, found, err := readFile(configPath)
	if err != nil {
		return nil, err
	}
	cfg.FirstRun = !found
	if found {
		if f.DBPath != nil {
			cfg.DBPath = *f.DBPath
		}
		if f.ContactEmail != nil {
			cfg.ContactEmail = *f.ContactEmail
		}
		if f.MaxNodes != nil {
			cfg.MaxNodes = *f.MaxNodes
		}
		if f.MaxDepth != nil {
			cfg.MaxDepth = *f.MaxDepth
		}
		if f.MaxRefsPerWork != nil {
			cfg.MaxRefsPerWork = *f.MaxRefsPerWork
		}
	}

	// Layer 2: the environment.
	if env := strings.TrimSpace(os.Getenv(EnvDB)); env != "" {
		cfg.DBPath = env
	}

	// Layer 1: the flag.
	if flag := strings.TrimSpace(opts.DBFlag); flag != "" {
		cfg.DBPath = flag
	}

	if err := cfg.normalise(); err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// EnvDB overrides the library path from the environment. It sits between the
// --db flag and the config file.
const EnvDB = "FILIATION_DB"

// readFile reads the config file if it is there. Absent is reported as not
// found rather than as an error, because absent is the ordinary first run.
func readFile(path string) (fileConfig, bool, error) {
	var f fileConfig
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, false, nil
	}
	if err != nil {
		return f, false, fmt.Errorf("read %s: %w", path, err)
	}
	md, err := toml.Decode(string(b), &f)
	if err != nil {
		return f, false, fmt.Errorf("parse %s: %v: %w", path, err, errs.ErrInvalidConfig)
	}
	// A key TOML parsed but this struct has no field for is almost always a
	// typo, and silence is the worst answer to it: max_node = 5000 would parse
	// cleanly, change nothing, and leave the user believing they had raised a
	// budget they had not. Refusing costs one clear message; accepting costs an
	// expansion nobody asked for.
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, 0, len(undecoded))
		for _, k := range undecoded {
			keys = append(keys, k.String())
		}
		return f, false, fmt.Errorf("unknown setting %s in %s: %w",
			strings.Join(keys, ", "), path, errs.ErrInvalidConfig)
	}
	return f, true, nil
}

// normalise expands a leading ~ and makes DBPath absolute. The tilde is handled
// because db_path is hand-edited by people who expect it to work, and Go does
// no shell expansion of its own.
func (c *Config) normalise() error {
	if strings.HasPrefix(c.DBPath, "~/") || strings.HasPrefix(c.DBPath, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("expand ~ in %q: %w", c.DBPath, err)
		}
		c.DBPath = filepath.Join(home, c.DBPath[2:])
	}
	abs, err := filepath.Abs(c.DBPath)
	if err != nil {
		return fmt.Errorf("resolve %q: %v: %w", c.DBPath, err, errs.ErrInvalidConfig)
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

// Save writes the config file, creating its directory if needed.
//
// It is how a location chosen on first run survives to the second. Choosing is
// not this package's job: config never prompts, because it has to work where
// there is no terminal. The front door asks; this records the answer.
func (c *Config) Save() error {
	if c.ConfigPath == "" {
		return fmt.Errorf("no config path set: %w", errs.ErrInvalidConfig)
	}
	if err := os.MkdirAll(filepath.Dir(c.ConfigPath), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	f := fileConfig{
		DBPath:         &c.DBPath,
		ContactEmail:   &c.ContactEmail,
		MaxNodes:       &c.MaxNodes,
		MaxDepth:       &c.MaxDepth,
		MaxRefsPerWork: &c.MaxRefsPerWork,
	}
	var b strings.Builder
	b.WriteString("# Filiation configuration.\n")
	b.WriteString("# Written on first run; edit freely. Delete a line to return it to its default.\n\n")
	if err := toml.NewEncoder(&b).Encode(f); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.WriteFile(c.ConfigPath, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", c.ConfigPath, err)
	}
	return nil
}
