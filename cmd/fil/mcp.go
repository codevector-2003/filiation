package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/mcpsrv"
)

func (a *app) mcpCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Serve your library to an AI assistant over MCP",
		Long: "Run an MCP server on standard input and output, so that an assistant such as\n" +
			"Claude Desktop, Claude Code or Cursor can add papers, expand the graph and ask\n" +
			"what cites what. Your MCP client starts this command; you do not run it yourself.\n\n" +
			"Claude Code:\n" +
			"  claude mcp add filiation -- fil mcp\n\n" +
			"Claude Desktop and most other clients, in their MCP settings file:\n" +
			"  \"filiation\": { \"command\": \"fil\", \"args\": [\"mcp\"] }\n\n" +
			"Use the full path to fil if it is not on your PATH.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.serveMCP(cmd.Context())
		},
	}
}

// serveMCP runs until the client disconnects. Standard output carries the
// protocol, so everything meant for a person goes to standard error.
func (a *app) serveMCP(ctx context.Context) error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	if cfg.FirstRun {
		// There is no one to ask where the library should go: the client owns
		// the terminal. Take the default, which --db or the settings file can
		// change, and say so where the client logs it.
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Fprintf(a.stderr, "fil: created your library at %s\n", cfg.DBPath)
	}

	lib, err := library.Open(ctx, cfg, library.Options{CacheDir: a.cacheDir, NoCache: a.noCache, Transport: a.transport})
	if err != nil {
		return err
	}
	defer lib.Close()

	fmt.Fprintf(a.stderr, "fil %s: serving %s over MCP\n", Version, cfg.DBPath)
	return mcpsrv.ServeStdio(ctx, lib, Version)
}
