package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/errs"
)

// Nothing in this file calls t.Parallel. FILIATION_DB is process-global, so a
// parallel test that sets it would be visible to every other test in the
// package. Correctness beats the milliseconds.

// write puts a config file in dir and returns dir, so a test can say what was
// on disk in one line.
func write(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return dir
}

// The whole point of the package: --db > FILIATION_DB > file > default
// (ADR-006).
func TestLoadPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		fileBody string // empty means no config file at all
		env      string
		flag     string
		want     string // expected DBPath, or a suffix if it starts with "..."
	}{
		{
			name: "default when nothing is set",
			want: "...library.db",
		},
		{
			name:     "file beats default",
			fileBody: `db_path = "/from/file.db"`,
			want:     "...file.db",
		},
		{
			name:     "env beats file",
			fileBody: `db_path = "/from/file.db"`,
			env:      "/from/env.db",
			want:     "...env.db",
		},
		{
			name:     "flag beats env",
			fileBody: `db_path = "/from/file.db"`,
			env:      "/from/env.db",
			flag:     "/from/flag.db",
			want:     "...flag.db",
		},
		{
			name: "flag beats default with no file",
			flag: "/from/flag.db",
			want: "...flag.db",
		},
		{
			name: "env beats default with no file",
			env:  "/from/env.db",
			want: "...env.db",
		},
		{
			name:     "blank env is not an override",
			fileBody: `db_path = "/from/file.db"`,
			env:      "   ",
			want:     "...file.db",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.fileBody != "" {
				dir = write(t, tt.fileBody)
			}
			t.Setenv(EnvDB, tt.env)

			cfg, err := Load(Options{DBFlag: tt.flag, Dir: dir})
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			want := strings.TrimPrefix(tt.want, "...")
			if !strings.HasSuffix(filepath.ToSlash(cfg.DBPath), want) {
				t.Errorf("DBPath = %q, want it to end in %q", cfg.DBPath, want)
			}
			if !filepath.IsAbs(cfg.DBPath) {
				t.Errorf("DBPath = %q, want an absolute path", cfg.DBPath)
			}
		})
	}
}

// Precedence is per field. A --db flag must not discard the budgets the user
// set in their config file.
func TestLoadPrecedenceIsPerField(t *testing.T) {
	dir := write(t, "db_path = \"/from/file.db\"\nmax_nodes = 42\n")
	t.Setenv(EnvDB, "")

	cfg, err := Load(Options{DBFlag: "/from/flag.db", Dir: dir})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !strings.HasSuffix(filepath.ToSlash(cfg.DBPath), "flag.db") {
		t.Errorf("DBPath = %q, want the flag to win", cfg.DBPath)
	}
	if cfg.MaxNodes != 42 {
		t.Errorf("MaxNodes = %d, want 42 — the flag must not discard the file", cfg.MaxNodes)
	}
}

// A field the file does not mention keeps its default. This is what the pointer
// fields in fileConfig buy: absent and zero are different answers.
func TestLoadFileOmissionsKeepDefaults(t *testing.T) {
	dir := write(t, "max_nodes = 42\n")
	t.Setenv(EnvDB, "")

	cfg, err := Load(Options{Dir: dir})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MaxNodes != 42 {
		t.Errorf("MaxNodes = %d, want 42", cfg.MaxNodes)
	}
	if cfg.MaxDepth != DefaultMaxDepth {
		t.Errorf("MaxDepth = %d, want the default %d", cfg.MaxDepth, DefaultMaxDepth)
	}
	if cfg.MaxRefsPerWork != DefaultMaxRefsPerWork {
		t.Errorf("MaxRefsPerWork = %d, want the default %d", cfg.MaxRefsPerWork, DefaultMaxRefsPerWork)
	}
}

// No config file is an ordinary first run, not a failure — and the CLI has to
// be able to tell, so it can offer a location and print where the library is.
func TestLoadFirstRun(t *testing.T) {
	t.Setenv(EnvDB, "")

	dir := t.TempDir()
	cfg, err := Load(Options{Dir: dir})
	if err != nil {
		t.Fatalf("Load() with no config file error = %v, want nil", err)
	}
	if !cfg.FirstRun {
		t.Error("FirstRun = false, want true when no config file exists")
	}
	if cfg.ConfigPath != filepath.Join(dir, FileName) {
		t.Errorf("ConfigPath = %q, want the path it looked in", cfg.ConfigPath)
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	again, err := Load(Options{Dir: dir})
	if err != nil {
		t.Fatalf("Load() after Save error = %v", err)
	}
	if again.FirstRun {
		t.Error("FirstRun = true after Save, want false")
	}
}

// Everything the user can get wrong must be refused here, wrapped in a sentinel
// the CLI can branch on — never silently corrected, because a typo in a budget
// would otherwise expand a graph nobody asked for.
func TestLoadRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"malformed toml", "max_nodes = \n"},
		{"unknown syntax", "this is not toml at all\n"},
		{"zero max_nodes", "max_nodes = 0\n"},
		{"negative max_nodes", "max_nodes = -1\n"},
		{"zero max_depth", "max_depth = 0\n"},
		{"zero max_refs_per_work", "max_refs_per_work = 0\n"},
		{"typo in a key name", "max_node = 5000\n"},
		{"unknown setting entirely", "colour = \"blue\"\n"},
		{"email without an at sign", "contact_email = \"not-an-address\"\n"},
		{"email with a space", "contact_email = \"a b@example.com\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvDB, "")
			_, err := Load(Options{Dir: write(t, tt.body)})
			if err == nil {
				t.Fatal("Load() error = nil, want an error")
			}
			if !errors.Is(err, errs.ErrInvalidConfig) {
				t.Errorf("Load() error = %v, want it to wrap errs.ErrInvalidConfig", err)
			}
		})
	}
}

// A valid address must survive, or the check above is too eager.
func TestLoadAcceptsContactEmail(t *testing.T) {
	t.Setenv(EnvDB, "")
	cfg, err := Load(Options{Dir: write(t, "contact_email = \"user@example.com\"\n")})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ContactEmail != "user@example.com" {
		t.Errorf("ContactEmail = %q, want user@example.com", cfg.ContactEmail)
	}
	if len(cfg.Warnings()) != 0 {
		t.Errorf("Warnings() = %v, want none when the email is set", cfg.Warnings())
	}
}

// Missing contact email warns but does not refuse: mailto produced no
// measurable benefit (D11), so blocking on it would punish politeness.
func TestWarningsOnMissingContactEmail(t *testing.T) {
	t.Setenv(EnvDB, "")
	cfg, err := Load(Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	w := cfg.Warnings()
	if len(w) != 1 {
		t.Fatalf("Warnings() = %v, want exactly one", w)
	}
	if !strings.Contains(w[0], cfg.ConfigPath) {
		t.Errorf("warning %q does not name the file to edit", w[0])
	}
}

// db_path is hand-edited, so a leading ~ has to work. Go does no shell
// expansion of its own.
func TestLoadExpandsTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	t.Setenv(EnvDB, "")

	cfg, err := Load(Options{Dir: write(t, "db_path = \"~/filiation-test.db\"\n")})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := filepath.Join(home, "filiation-test.db")
	if cfg.DBPath != want {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, want)
	}
}

// What Save writes, Load must read back unchanged. This is the path a location
// chosen on first run travels.
func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv(EnvDB, "")
	dir := t.TempDir()

	cfg, err := Load(Options{Dir: dir})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	cfg.DBPath = filepath.Join(t.TempDir(), "chosen.db")
	cfg.ContactEmail = "user@example.com"
	cfg.MaxNodes = 250
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Load(Options{Dir: dir})
	if err != nil {
		t.Fatalf("Load() after Save error = %v", err)
	}
	if got.DBPath != cfg.DBPath {
		t.Errorf("DBPath = %q, want %q", got.DBPath, cfg.DBPath)
	}
	if got.ContactEmail != cfg.ContactEmail {
		t.Errorf("ContactEmail = %q, want %q", got.ContactEmail, cfg.ContactEmail)
	}
	if got.MaxNodes != 250 {
		t.Errorf("MaxNodes = %d, want 250", got.MaxNodes)
	}
}

// Save must create the directory it was pointed at, because the per-user
// config directory does not exist before the first run.
func TestSaveCreatesDirectory(t *testing.T) {
	t.Setenv(EnvDB, "")
	dir := filepath.Join(t.TempDir(), "nested", "filiation")

	cfg, err := Load(Options{Dir: dir})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Errorf("config file not written: %v", err)
	}
}

// The default location must be per-user, not per-directory (ADR-006), and must
// be namespaced so it does not scatter files into a shared config root.
func TestDefaultDir(t *testing.T) {
	dir, err := DefaultDir()
	if err != nil {
		t.Skipf("no user config directory: %v", err)
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("DefaultDir() = %q, want an absolute path", dir)
	}
	if filepath.Base(dir) != "filiation" {
		t.Errorf("DefaultDir() = %q, want it to end in filiation", dir)
	}
}
