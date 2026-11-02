package assistants

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func testEnv(t *testing.T) Env {
	t.Helper()
	dir := t.TempDir()
	return Env{Home: filepath.Join(dir, "home"), ConfigDir: filepath.Join(dir, "config"), Command: "fil",
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		Run: func(string, ...string) (string, error) {
			t.Error("a command ran that the test did not expect")
			return "", nil
		}}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func statusOf(t *testing.T, e Env, id string) Status {
	t.Helper()
	for _, s := range List(e) {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no status for %s", id)
	return Status{}
}

func TestConnectKeepsEverythingElseAndABackup(t *testing.T) {
	t.Parallel()
	e := testEnv(t)
	path := filepath.Join(e.ConfigDir, "Claude", "claude_desktop_config.json")
	before := `{"theme":"dark","big":12345678901234567890,"mcpServers":{"other":{"command":"x"}}}`
	write(t, path, before)

	if s := statusOf(t, e, "claude-desktop"); !s.Installed || s.Connected {
		t.Fatalf("before = %+v", s)
	}
	if err := Connect(e, "claude-desktop", false); err != nil {
		t.Fatal(err)
	}
	if s := statusOf(t, e, "claude-desktop"); !s.Connected || s.ReadOnly {
		t.Errorf("after = %+v", s)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"theme": "dark"`, "12345678901234567890", `"other"`, `"filiation"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("settings lost %s: %s", want, b)
		}
	}
	if old, _ := os.ReadFile(path + BackupSuffix); string(old) != before {
		t.Errorf("backup = %q", old)
	}

	if err := Disconnect(e, "claude-desktop"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), ServerName) || !strings.Contains(string(b), `"other"`) {
		t.Errorf("after disconnect: %s", b)
	}
}

func TestConnectCreatesAMissingFile(t *testing.T) {
	t.Parallel()
	e := testEnv(t)
	if err := Connect(e, "cursor", true); err != nil {
		t.Fatal(err)
	}
	if s := statusOf(t, e, "cursor"); !s.Connected || !s.ReadOnly {
		t.Errorf("status = %+v", s)
	}
}

func TestVSCodeNamesItsTransport(t *testing.T) {
	t.Parallel()
	e := testEnv(t)
	if err := Connect(e, "vscode", false); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(e.ConfigDir, "Code", "User", "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Servers map[string]struct {
			Type string `json:"type"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(b, &doc); err != nil || doc.Servers[ServerName].Type != "stdio" {
		t.Errorf("mcp.json = %s, %v", b, err)
	}
}

// A file with comments cannot be written back without losing them.
func TestFileWithCommentsIsLeftAlone(t *testing.T) {
	t.Parallel()
	e := testEnv(t)
	path := filepath.Join(e.Home, ".gemini", "settings.json")
	before := "{\n  // my settings\n  \"theme\": \"dark\"\n}\n"
	write(t, path, before)
	if err := Connect(e, "gemini", false); err == nil {
		t.Error("Connect changed a file it cannot read exactly")
	}
	if b, _ := os.ReadFile(path); string(b) != before {
		t.Errorf("the file changed: %s", b)
	}
	if s := statusOf(t, e, "gemini"); s.Problem == "" {
		t.Errorf("status = %+v; want the problem said", s)
	}
}

func TestUnexpectedSectionIsLeftAlone(t *testing.T) {
	t.Parallel()
	e := testEnv(t)
	path := filepath.Join(e.Home, ".cursor", "mcp.json")
	write(t, path, `{"mcpServers":[1,2]}`)
	if err := Connect(e, "cursor", false); err == nil {
		t.Error("Connect replaced a section it does not understand")
	}
}

func TestClaudeCodeGoesThroughItsOwnCommand(t *testing.T) {
	t.Parallel()
	e := testEnv(t)
	e.LookPath = func(string) (string, error) { return "claude", nil }
	var ran [][]string
	e.Run = func(name string, args ...string) (string, error) {
		ran = append(ran, append([]string{name}, args...))
		return "", nil
	}
	if err := Connect(e, "claude-code", true); err != nil {
		t.Fatal(err)
	}
	last := ran[len(ran)-1]
	if !slices.Contains(last, "add") || last[len(last)-1] != readOnlyFlag {
		t.Errorf("ran %q", last)
	}
	write(t, filepath.Join(e.Home, ".claude.json"), `{"mcpServers":{"filiation":{"args":["mcp","--read-only"]}}}`)
	if s := statusOf(t, e, "claude-code"); !s.Connected || !s.ReadOnly {
		t.Errorf("status = %+v", s)
	}
}

func TestCallLog(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sub", CallLogName)
	for _, tool := range []string{"one", "two", "three"} {
		Record(path, Call{At: time.Now(), Tool: tool})
	}
	got := Recent(path, 2)
	if len(got) != 2 || got[0].Tool != "three" || got[1].Tool != "two" {
		t.Errorf("Recent = %+v", got)
	}
	if got := Recent(filepath.Join(t.TempDir(), "none"), 5); len(got) != 0 {
		t.Errorf("Recent of no log = %+v", got)
	}
}
