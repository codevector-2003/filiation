package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// on is a coloured paint, as a terminal would get.
func on(t theme) paint { return paint{on: true, p: t.dark, depth: 24} }

func TestPlainPaintChangesNothing(t *testing.T) {
	var p paint
	for _, f := range []func(string) string{p.title, p.accent, p.label, p.id, p.num, p.ok, p.bad, p.warn, p.dim, p.hit} {
		if got := f("F45ZSWF64"); got != "F45ZSWF64" {
			t.Errorf("plain paint gave %q", got)
		}
	}
	var buf bytes.Buffer
	text := "Works:      48 — F45ZSWF64 W2741809807 doi:10.7717/peerj.4375 «hybrid»\n"
	styled{w: &buf, p: p}.Write([]byte(text))
	if buf.String() != text {
		t.Errorf("plain styling changed the output: %q", buf.String())
	}
}

func TestStyledColoursTheShapes(t *testing.T) {
	p := on(themes[0])
	var buf bytes.Buffer
	styled{w: &buf, p: p}.Write([]byte("Works:      48\n  F45ZSWF64 W2741809807 doi:10.7717/peerj.4375\n…the «citation advantage» of…\n"))
	got := buf.String()
	for _, want := range []string{p.label("Works:"), p.id("F45ZSWF64"), p.dim("W2741809807"), p.dim("doi:10.7717/peerj.4375"), p.hit("citation advantage")} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.ContainsAny(got, "«»") {
		t.Errorf("the guillemets stayed: %q", got)
	}
}

// A title that starts a line with a colon in it is not a label.
func TestStyledLeavesTitlesAlone(t *testing.T) {
	p := on(themes[0])
	var buf bytes.Buffer
	line := "The state of OA: a large-scale analysis (2018, article)\n"
	styled{w: &buf, p: p}.Write([]byte(line))
	if buf.String() != line {
		t.Errorf("a title was styled as a label: %q", buf.String())
	}
}

func TestEveryThemeHasEveryColour(t *testing.T) {
	for _, th := range themes {
		for _, pal := range []palette{th.dark, th.light} {
			for _, c := range []rgb{pal.accent, pal.id, pal.num, pal.ok, pal.bad, pal.warn, pal.dim, pal.hitFg, pal.hitBg} {
				if c == (rgb{}) {
					t.Errorf("%s has an unset colour", th.name)
				}
			}
		}
		if _, ok := themeNamed(strings.ToUpper(th.name)); !ok {
			t.Errorf("%s is not found by name, ignoring case", th.name)
		}
	}
}

func TestXterm256(t *testing.T) {
	tests := map[rgb]int{
		{0, 0, 0}:       16,
		{255, 255, 255}: 231,
		{255, 0, 0}:     196,
		{128, 128, 128}: 244,
	}
	for c, want := range tests {
		if got := c.xterm256(); got != want {
			t.Errorf("%v → %d, want %d", c, got, want)
		}
	}
}

func TestDarkBackground(t *testing.T) {
	t.Setenv("COLORFGBG", "0;15")
	if darkBackground("") {
		t.Error("COLORFGBG 0;15 is a light background")
	}
	if !darkBackground("dark") {
		t.Error("the setting overrides COLORFGBG")
	}
	t.Setenv("COLORFGBG", "15;0")
	if !darkBackground("") || darkBackground("light") {
		t.Error("COLORFGBG 15;0 is dark, unless the setting says light")
	}
}

// Colour never reaches anything but a terminal.
func TestPaintForIsPlainOffATerminal(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if paintFor(f, false, "graph", "").on {
		t.Error("a file got colour")
	}
	if paintFor(nil, false, "graph", "").on {
		t.Error("no file got colour")
	}
}

// Plain progress is exactly what fil printed before themes.
func TestPlainStatusIsUnchanged(t *testing.T) {
	var buf bytes.Buffer
	a := &app{stderr: &buf}
	a.setStatus("read 3 of 9 PDFs")
	a.endStatus()
	if buf.String() != "\r  read 3 of 9 PDFs \n" {
		t.Errorf("plain status = %q", buf.String())
	}
}

func TestStatusInColourSpinsAndFinishes(t *testing.T) {
	var buf bytes.Buffer
	a := &app{stderr: &buf, errPaint: on(themes[0])}
	a.setStatus("fetched 3 of up to 10")
	a.endStatus()
	got := buf.String()
	if !strings.Contains(got, "fetched 3 of up to 10") || !strings.Contains(got, "✓") || !strings.HasSuffix(got, "\n") {
		t.Errorf("colour status = %q", got)
	}
}

func TestThemeCommand(t *testing.T) {
	h := newHarness(t, nil)
	code, out, _ := h.run(false, "", "theme")
	if code != exitOK || !strings.Contains(out, "* lineage") || !strings.Contains(out, "manuscript") || !strings.Contains(out, "graph") {
		t.Fatalf("theme = %d, %q", code, out)
	}

	code, out, _ = h.run(false, "", "theme", "Manuscript")
	if code != exitOK || !strings.Contains(out, "Theme: manuscript") {
		t.Fatalf("theme manuscript = %d, %q", code, out)
	}
	settings, err := os.ReadFile(filepath.Join(h.configDir, "filiation.toml"))
	if err != nil || !strings.Contains(string(settings), `theme = "manuscript"`) {
		t.Fatalf("settings = %q, %v", settings, err)
	}
	if _, err := os.Stat(h.documentsDir); err == nil {
		t.Error("choosing a theme made a library")
	}

	code, out, _ = h.run(false, "", "theme", "--background", "light")
	if code != exitOK || !strings.Contains(out, "Theme: manuscript, for a light background") {
		t.Errorf("--background light = %d, %q", code, out)
	}
	code, out, _ = h.run(false, "", "theme")
	if !strings.Contains(out, "* manuscript") || !strings.Contains(out, "Background: light") {
		t.Errorf("theme after choosing = %d, %q", code, out)
	}

	if code, _, errOut := h.run(false, "", "theme", "neon"); code != exitInvalidInput || !strings.Contains(errOut, `no theme "neon"`) {
		t.Errorf("unknown theme = %d, %q", code, errOut)
	}
	if code, _, _ := h.run(false, "", "theme", "--background", "beige"); code != exitInvalidInput {
		t.Errorf("unknown background = %d", code)
	}
}

// --plain is accepted by every command.
func TestPlainFlag(t *testing.T) {
	h := newHarness(t, nil)
	if code, out, _ := h.run(false, "", "--plain", "version"); code != exitOK || !strings.Contains(out, "fil ") {
		t.Errorf("--plain version = %d, %q", code, out)
	}
}
