package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// promptLine asks for one line in a box at the foot of the terminal, with a
// menu of commands under it while the line starts with "/". It returns the
// line, or false when the person leaves (Ctrl+C or Ctrl+D on an empty line).
func (a *app) promptLine(history []string) (string, bool, error) {
	m := newPrompt(a.sessionCommands(), history, a.outPaint)
	final, err := tea.NewProgram(m, tea.WithOutput(a.raw())).Run()
	if err != nil {
		return "", false, fmt.Errorf("prompt: %w", err)
	}
	pm := final.(promptModel)
	if pm.left {
		return "", false, nil
	}
	return pm.value, true, nil
}

// menuRows is the most commands the menu shows at once.
const menuRows = 8

// promptModel is the prompt: a one-line input, its command menu, and the
// history the arrow keys walk.
type promptModel struct {
	input    textinput.Model
	commands []sessionCommand
	history  []string
	histAt   int // len(history) while typing a new line
	draft    string

	matches []sessionCommand
	sel     int

	paint  paint
	width  int
	done   bool
	left   bool
	value  string
}

func newPrompt(commands []sessionCommand, history []string, p paint) promptModel {
	in := textinput.New()
	in.Prompt = "› "
	in.Placeholder = "a command, or / for the list"
	in.Focus()
	in.CharLimit = 2000
	if p.on {
		accent := lipgloss.Color(p.p.accent.hexString())
		in.PromptStyle = lipgloss.NewStyle().Foreground(accent).Bold(true)
		in.Cursor.Style = lipgloss.NewStyle().Foreground(accent)
		in.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(p.p.dim.hexString()))
	}
	return promptModel{input: in, commands: commands, history: history, histAt: len(history), paint: p, width: 80}
}

func (m promptModel) Init() tea.Cmd { return textinput.Blink }

func (m promptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			if m.input.Value() == "" {
				m.done, m.left = true, true
				return m, tea.Quit
			}
			m.input.SetValue("")
			m.matches = nil
			return m, nil
		case tea.KeyCtrlD:
			if m.input.Value() == "" {
				m.done, m.left = true, true
				return m, tea.Quit
			}
		case tea.KeyEsc:
			m.input.SetValue("")
			m.matches = nil
			return m, nil
		case tea.KeyTab:
			if len(m.matches) > 0 {
				m.complete()
			}
			return m, nil
		case tea.KeyUp:
			if len(m.matches) > 0 {
				m.sel = (m.sel + len(m.matches) - 1) % len(m.matches)
			} else {
				m.back()
			}
			return m, nil
		case tea.KeyDown:
			if len(m.matches) > 0 {
				m.sel = (m.sel + 1) % len(m.matches)
			} else {
				m.forward()
			}
			return m, nil
		case tea.KeyEnter:
			// A half-typed command name in the menu completes first, so
			// "/sea" then Enter gives "/search ", ready for its words.
			if len(m.matches) > 0 && !m.isCommand() {
				m.complete()
				return m, nil
			}
			m.value = strings.TrimSpace(m.input.Value())
			m.done = true
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.refreshMenu()
	return m, cmd
}

// refreshMenu lists the commands matching what is typed, while the line is
// a "/" and a command name with nothing after it yet.
func (m *promptModel) refreshMenu() {
	v := m.input.Value()
	if !strings.HasPrefix(v, "/") || strings.ContainsAny(v, " \t") {
		m.matches = nil
		return
	}
	word := strings.ToLower(strings.TrimPrefix(v, "/"))
	var out []sessionCommand
	for _, c := range m.commands {
		if strings.HasPrefix(c.name, word) {
			out = append(out, c)
		}
	}
	m.matches = out
	if m.sel >= len(out) {
		m.sel = 0
	}
}

// isCommand reports whether the line names a command exactly.
func (m promptModel) isCommand() bool {
	word := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(m.input.Value()), "/"))
	for _, c := range m.commands {
		if c.name == word {
			return true
		}
	}
	return false
}

func (m *promptModel) complete() {
	m.input.SetValue("/" + m.matches[m.sel].name + " ")
	m.input.CursorEnd()
	m.matches, m.sel = nil, 0
}

func (m *promptModel) back() {
	if m.histAt == 0 {
		return
	}
	if m.histAt == len(m.history) {
		m.draft = m.input.Value()
	}
	m.histAt--
	m.input.SetValue(m.history[m.histAt])
	m.input.CursorEnd()
}

func (m *promptModel) forward() {
	if m.histAt >= len(m.history) {
		return
	}
	m.histAt++
	if m.histAt == len(m.history) {
		m.input.SetValue(m.draft)
	} else {
		m.input.SetValue(m.history[m.histAt])
	}
	m.input.CursorEnd()
}

func (m promptModel) View() string {
	p := m.paint
	if m.done {
		if m.left {
			return ""
		}
		// What stays on screen once the line is sent: the line, as typed.
		return p.label("›") + " " + m.value + "\n"
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Width(max(20, m.width-2))
	if p.on {
		box = box.BorderForeground(lipgloss.Color(p.p.accent.hexString()))
	}
	var b strings.Builder
	b.WriteString(box.Render(m.input.View()))
	b.WriteString("\n")
	if len(m.matches) > 0 {
		start := 0
		if m.sel >= menuRows {
			start = m.sel - menuRows + 1
		}
		end := min(len(m.matches), start+menuRows)
		for i := start; i < end; i++ {
			c := m.matches[i]
			name := fmt.Sprintf("/%-11s", c.name)
			if i == m.sel {
				b.WriteString("  " + p.label("› "+name) + " " + c.short + "\n")
			} else {
				b.WriteString("    " + p.accent(name) + " " + p.dim(c.short) + "\n")
			}
		}
		if len(m.matches) > end-start {
			b.WriteString(p.dim(fmt.Sprintf("    … %d more; keep typing\n", len(m.matches)-(end-start))))
		}
	}
	b.WriteString(p.dim("  / commands · Tab completes · ↑↓ history · Esc clears · Ctrl+C on an empty line leaves"))
	return b.String()
}
