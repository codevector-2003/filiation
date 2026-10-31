package main

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// A theme is a colour scheme for fil in a terminal. Each has a palette for a
// dark background and one for a light background, chosen by the terminal's
// background, so it reads on either. Colour never reaches a pipe, a file,
// a test, NO_COLOR or --plain: there fil prints exactly what it always has.
type theme struct {
	name, about string
	dark, light palette
}

// palette is the handful of roles fil colours. Everything else is the
// terminal's own colour.
type palette struct {
	accent rgb // labels, the prompt, the spinner
	id     rgb // fil IDs, pages
	num    rgb // counts that matter
	ok     rgb // ✓, searchable, downloaded
	bad    rgb // ✗, refused, errors
	warn   rgb // !, worth a look
	dim    rgb // OpenAlex IDs, DOIs, secondary detail
	hitFg  rgb // a matched word in a search snippet
	hitBg  rgb
}

type rgb struct{ r, g, b uint8 }

func hex(s string) rgb {
	v, _ := strconv.ParseUint(strings.TrimPrefix(s, "#"), 16, 32)
	return rgb{uint8(v >> 16), uint8(v >> 8), uint8(v)}
}

// themes are the schemes a person can choose with fil theme. The first is
// the default.
var themes = []theme{
	{
		name:  "lineage",
		about: "indigo and teal, matching the logo",
		dark: palette{accent: hex("8E96FF"), id: hex("5CCFC3"), num: hex("E6E6E3"), ok: hex("7BD88F"), bad: hex("FF8A80"),
			warn: hex("F2C66D"), dim: hex("8B8F97"), hitFg: hex("14161B"), hitBg: hex("8E96FF")},
		light: palette{accent: hex("4F46E5"), id: hex("0F766E"), num: hex("1D1F23"), ok: hex("1F7A3A"), bad: hex("B42318"),
			warn: hex("8A5300"), dim: hex("5B5F66"), hitFg: hex("FFFFFF"), hitBg: hex("4F46E5")},
	},
	{
		name:  "manuscript",
		about: "warm amber and olive, like old paper and ink",
		dark: palette{accent: hex("E8A957"), id: hex("B9C27A"), num: hex("F1E6D2"), ok: hex("9CCB6E"), bad: hex("F08A6C"),
			warn: hex("E8C547"), dim: hex("958C7E"), hitFg: hex("1B1712"), hitBg: hex("E8A957")},
		light: palette{accent: hex("9A5B13"), id: hex("5E6B1E"), num: hex("2B241B"), ok: hex("3C6E1F"), bad: hex("A3341C"),
			warn: hex("7A5A00"), dim: hex("6E6558"), hitFg: hex("FFFFFF"), hitBg: hex("9A5B13")},
	},
	{
		name:  "graph",
		about: "cyan and magenta, high contrast",
		dark: palette{accent: hex("4FD6F2"), id: hex("F27FD0"), num: hex("E6E6E3"), ok: hex("6EE7A0"), bad: hex("FF7A8A"),
			warn: hex("FFD166"), dim: hex("8B8F97"), hitFg: hex("14161B"), hitBg: hex("4FD6F2")},
		light: palette{accent: hex("0B7A99"), id: hex("A3227E"), num: hex("1D1F23"), ok: hex("18794E"), bad: hex("B4233C"),
			warn: hex("8A5A00"), dim: hex("5B5F66"), hitFg: hex("FFFFFF"), hitBg: hex("0B7A99")},
	},
}

// themeNamed finds a theme by name, ignoring case.
func themeNamed(name string) (theme, bool) {
	for _, t := range themes {
		if strings.EqualFold(t.name, strings.TrimSpace(name)) {
			return t, true
		}
	}
	return theme{}, false
}

func themeNames() []string {
	names := make([]string, len(themes))
	for i, t := range themes {
		names[i] = t.name
	}
	return names
}

// EnvTheme overrides the saved theme for one run; "plain" turns colour off.
const EnvTheme = "FIL_THEME"

// paint colours text for one output. Its zero value is plain: every method
// returns its argument unchanged.
type paint struct {
	on    bool
	p     palette
	depth int // 24 for true colour, 8 for the 256-colour table
}

const (
	sgrReset = "\x1b[0m"
	sgrBold  = "\x1b[1m"
)

func (c rgb) fg(depth int) string {
	if depth == 24 {
		return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.r, c.g, c.b)
	}
	return fmt.Sprintf("\x1b[38;5;%dm", c.xterm256())
}

func (c rgb) bg(depth int) string {
	if depth == 24 {
		return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", c.r, c.g, c.b)
	}
	return fmt.Sprintf("\x1b[48;5;%dm", c.xterm256())
}

// xterm256 is the nearest colour in the 6×6×6 cube or the grey ramp, for
// terminals without true colour (macOS Terminal among them).
func (c rgb) xterm256() int {
	level := func(v uint8) int {
		if v < 48 {
			return 0
		}
		if v < 115 {
			return 1
		}
		return (int(v) - 35) / 40
	}
	steps := [6]int{0, 95, 135, 175, 215, 255}
	r, g, b := level(c.r), level(c.g), level(c.b)
	cube := 16 + 36*r + 6*g + b
	sq := func(a, b int) int { return (a - b) * (a - b) }
	cubeDist := sq(steps[r], int(c.r)) + sq(steps[g], int(c.g)) + sq(steps[b], int(c.b))
	avg := (int(c.r) + int(c.g) + int(c.b)) / 3
	grey := min(23, max(0, (avg-3)/10))
	gv := 8 + 10*grey
	if sq(gv, int(c.r))+sq(gv, int(c.g))+sq(gv, int(c.b)) < cubeDist {
		return 232 + grey
	}
	return cube
}

func (p paint) wrap(code, s string) string {
	if !p.on || s == "" {
		return s
	}
	return code + s + sgrReset
}

func (p paint) title(s string) string  { return p.wrap(sgrBold, s) }
func (p paint) accent(s string) string { return p.wrap(p.p.accent.fg(p.depth), s) }
func (p paint) label(s string) string  { return p.wrap(sgrBold+p.p.accent.fg(p.depth), s) }
func (p paint) id(s string) string     { return p.wrap(p.p.id.fg(p.depth), s) }
func (p paint) num(s string) string    { return p.wrap(sgrBold+p.p.num.fg(p.depth), s) }
func (p paint) ok(s string) string     { return p.wrap(p.p.ok.fg(p.depth), s) }
func (p paint) bad(s string) string    { return p.wrap(p.p.bad.fg(p.depth), s) }
func (p paint) warn(s string) string   { return p.wrap(p.p.warn.fg(p.depth), s) }
func (p paint) dim(s string) string    { return p.wrap(p.p.dim.fg(p.depth), s) }
func (p paint) hit(s string) string {
	return p.wrap(p.p.hitBg.bg(p.depth)+p.p.hitFg.fg(p.depth), s)
}

// paintFor decides whether output to f is coloured, and how: only a terminal
// gets colour, and not when NO_COLOR is set, TERM is dumb, --plain was given,
// or the theme is "plain". f is nil in tests.
func paintFor(f *os.File, plain bool, themeName, background string) paint {
	if f == nil || plain || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" || !isTerminal(f) {
		return paint{}
	}
	if env := os.Getenv(EnvTheme); env != "" {
		themeName = env
	}
	if strings.EqualFold(themeName, "plain") {
		return paint{}
	}
	t, ok := themeNamed(themeName)
	if !ok {
		t = themes[0]
	}
	if !enableColour(f) {
		return paint{}
	}
	p := paint{on: true, p: t.dark, depth: colourDepth()}
	if !darkBackground(background) {
		p.p = t.light
	}
	return p
}

// colourDepth is 24 where true colour is known to work, else 8: Windows'
// console has had it since Windows 10; elsewhere COLORTERM says so.
func colourDepth() int {
	ct := strings.ToLower(os.Getenv("COLORTERM"))
	switch {
	case ct == "truecolor" || ct == "24bit":
		return 24
	case os.Getenv("TERM_PROGRAM") == "Apple_Terminal":
		return 8
	case runtime.GOOS == "windows":
		return 24
	}
	return 8
}

// darkBackground reports whether to use a theme's dark palette: what the
// settings say, else what COLORFGBG says ("15;0" is light text on a dark
// background), else dark, which most terminals are.
func darkBackground(setting string) bool {
	switch strings.ToLower(setting) {
	case "dark":
		return true
	case "light":
		return false
	}
	if v := os.Getenv("COLORFGBG"); v != "" {
		parts := strings.Split(v, ";")
		if bg, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			return bg != 7 && bg != 15
		}
	}
	return true
}

// styled colours the regular shapes in fil's output as it is written, so
// every command gets the theme without each one being touched: the labels
// that start lines, fil IDs, OpenAlex IDs, DOIs, and the «matched words» of
// a search. Writes pass straight through when the paint is plain.
type styled struct {
	w io.Writer
	p paint
}

var (
	labelLine = regexp.MustCompile(`(?m)^(Library|Seeds|Works|Citations|Coverage|PDFs?|Text|Context|Duplicates|Inbox|Added|References|OpenAlex|Fetched|Not found|Stopped|In the library|Graph|Folder|Imported|Filed|Looked up|Not looked up|Skipped|Settings|Database|Cache|Collections|Read|Merged|Downloaded|Details|Cited by|Its title|No PDF|Already in your library|Why each is waiting|Nothing to read|Nothing to import|Nothing to download):`)
	filID      = regexp.MustCompile(`\bF[0-9A-HJKMNP-TV-Z]{8}\b`)
	openalexID = regexp.MustCompile(`\bW\d{5,}\b`)
	doiRef     = regexp.MustCompile(`\bdoi:\S+`)
	searchHit  = regexp.MustCompile(`«([^»]*)»`)
	errLine    = regexp.MustCompile(`(?m)^fil:`)
)

func (s styled) Write(b []byte) (int, error) {
	if !s.p.on {
		return s.w.Write(b)
	}
	t := string(b)
	t = searchHit.ReplaceAllStringFunc(t, func(m string) string { return s.p.hit(m[len("«") : len(m)-len("»")]) })
	t = labelLine.ReplaceAllStringFunc(t, s.p.label)
	t = errLine.ReplaceAllStringFunc(t, func(m string) string { return s.p.wrap(sgrBold+s.p.p.bad.fg(s.p.depth), m) })
	t = filID.ReplaceAllStringFunc(t, s.p.id)
	t = openalexID.ReplaceAllStringFunc(t, s.p.dim)
	t = doiRef.ReplaceAllStringFunc(t, s.p.dim)
	if _, err := io.WriteString(s.w, t); err != nil {
		return 0, err
	}
	return len(b), nil
}
