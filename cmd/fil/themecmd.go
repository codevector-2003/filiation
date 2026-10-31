package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codevector-2003/filiation/internal/config"
)

func (a *app) themeCommand() *cobra.Command {
	var background string
	cmd := &cobra.Command{
		Use:   "theme [" + strings.Join(themeNames(), " | ") + " | plain]",
		Short: "Choose fil's colours in the terminal",
		Long: "Show the colour schemes, or choose one. Each has colours for a dark and a light\n" +
			"terminal background; fil guesses which you have, and --background settles it.\n\n" +
			"Colour only ever reaches a terminal. Piped or redirected output, --json and\n" +
			"files stay plain, and so does everything when NO_COLOR is set or --plain is\n" +
			"given. FIL_THEME overrides the saved choice for one run.",
		Example: "  fil theme\n  fil theme manuscript\n  fil theme graph --background light\n  fil theme plain",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return usageError{cmd: cmd, err: errors.New("theme takes one name")}
			}
			if len(args) == 1 && !strings.EqualFold(args[0], "plain") {
				if _, ok := themeNamed(args[0]); !ok {
					return usageError{cmd: cmd, err: fmt.Errorf("no theme %q: choose %s or plain", args[0], strings.Join(themeNames(), ", "))}
				}
			}
			switch strings.ToLower(background) {
			case "", "auto", "dark", "light":
			default:
				return usageError{cmd: cmd, err: errors.New("--background must be auto, dark or light")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && background == "" {
				return a.showThemes()
			}
			name := ""
			if len(args) == 1 {
				name = strings.ToLower(args[0])
			}
			return a.saveTheme(name, strings.ToLower(background))
		},
	}
	cmd.Flags().StringVar(&background, "background", "", "your terminal's background: auto, dark or light")
	return cmd
}

// themeSettings reads the saved theme without opening a library, so colour
// is decided before a command runs and works before the first library exists.
func (a *app) themeSettings() *config.Config {
	cfg, err := config.Load(config.Options{Dir: a.configDir, DocumentsDir: a.documentsDir})
	if err != nil {
		return &config.Config{}
	}
	return cfg
}

func (a *app) showThemes() error {
	cfg := a.themeSettings()
	current := cfg.Theme
	if current == "" {
		current = themes[0].name
	}
	w := a.stdout
	fmt.Fprintln(w, "Themes for fil in the terminal:")
	fmt.Fprintln(w)
	for _, t := range themes {
		mark := " "
		if strings.EqualFold(t.name, current) {
			mark = "*"
		}
		fmt.Fprintf(w, "%s %-11s %s\n", mark, t.name, t.about)
		if a.outPaint.on {
			p := a.outPaint
			p.p = t.dark
			if !darkBackground(cfg.ThemeBackground) {
				p.p = t.light
			}
			fmt.Fprintf(w, "              %s %s %s %s %s %s\n\n",
				p.label("Works:"), p.id("F45ZSWF64"), p.hit("citation advantage"), p.ok("✓ downloaded"),
				p.bad("✗ refused"), p.warn("! web page only"))
		}
	}
	if strings.EqualFold(current, "plain") {
		fmt.Fprintln(w, "* plain        no colour")
	}
	bg := cfg.ThemeBackground
	if bg == "" {
		bg = "auto"
	}
	fmt.Fprintf(w, "\nBackground: %s. Choose with fil theme <name>, and --background dark or light\n", bg)
	fmt.Fprintln(w, "if the colours look washed out.")
	return nil
}

// saveTheme records a theme, a background ("auto" means guess), or both; an
// empty name keeps the theme already chosen.
func (a *app) saveTheme(name, background string) error {
	cfg, err := config.Load(config.Options{Dir: a.configDir, DocumentsDir: a.documentsDir})
	if err != nil {
		return err
	}
	if name == "" {
		name = cfg.Theme
		if name == "" {
			name = themes[0].name
		}
	}
	if err := cfg.SaveTheme(name, background); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Theme: %s", name)
	if background != "" && background != "auto" {
		fmt.Fprintf(a.stdout, ", for a %s background", background)
	}
	fmt.Fprintf(a.stdout, ". Saved in %s\n", cfg.ConfigPath)
	return nil
}
