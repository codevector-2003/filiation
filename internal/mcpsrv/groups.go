package mcpsrv

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/codevector-2003/filiation/internal/library"
)

// Limits for citation_groups.
const (
	groupTopPapers     = 5   // papers named per group in the list
	defaultGroupPapers = 50  // papers of one group
	maxGroupPapers     = 200 // papers of one group, at most
)

func addGroupTools(s *mcp.Server, t tools, readOnly func(string) *mcp.ToolAnnotations) {
	mcp.AddTool(s, &mcp.Tool{
		Name:  "citation_groups",
		Title: "Topics found from citations",
		Description: "The topics in the library, found from its citations alone: groups of papers " +
			"that cite one another more than the rest, each named after the paper its members cite " +
			"most. No model guesses what papers are about. Only papers linked to at least two others " +
			"are grouped, so few reference lists (common outside STEM) mean few groups. Without group, " +
			"lists the groups and their most cited papers; with group (its number, name or anchor " +
			"fil_id), lists that group's papers. detail is broad, normal or fine.",
		Annotations: readOnly("Topics found from citations"),
	}, t.citationGroups)
}

type groupsIn struct {
	Group  string `json:"group,omitempty" jsonschema:"one group to list the papers of: its number from a previous call, its name, or its anchor's fil_id"`
	Detail string `json:"detail,omitempty" jsonschema:"broad (fewer, larger groups), normal (the default) or fine (more, smaller groups)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"with group, how many of its papers (default 50, at most 200)"`
}

// GroupPaper is a paper in a citation group.
type GroupPaper struct {
	FilID       string `json:"fil_id"`
	Title       string `json:"title,omitempty"`
	FirstAuthor string `json:"first_author,omitempty"`
	Year        int    `json:"year,omitempty"`
	Stub        bool   `json:"stub" jsonschema:"not fetched: its title may be known, its content is not"`
	CitedWithin int    `json:"cited_within_group" jsonschema:"how many papers of the group cite it"`
}

// CitationGroup is one group found.
type CitationGroup struct {
	Number   int          `json:"number" jsonschema:"its place in the list, largest first, at this detail"`
	Name     string       `json:"name"`
	Anchor   string       `json:"anchor" jsonschema:"fil_id of the paper the group's members cite most"`
	Papers   int          `json:"papers"`
	Fetched  int          `json:"fetched"`
	YearFrom int          `json:"year_from,omitempty"`
	YearTo   int          `json:"year_to,omitempty"`
	Top      []GroupPaper `json:"papers_list" jsonschema:"its most cited papers, or with group, its papers"`
	More     int          `json:"more_papers,omitempty" jsonschema:"papers in the group not listed"`
}

type groupsOut struct {
	Groups []CitationGroup `json:"groups"`
	Detail string          `json:"detail"`
	Linked int             `json:"papers_grouped_from" jsonschema:"papers linked to at least two others, which were grouped"`
	Fringe int             `json:"papers_not_grouped" jsonschema:"papers linked to only one other, left out"`
	Note   string          `json:"note,omitempty"`
}

func (t tools) citationGroups(ctx context.Context, _ *mcp.CallToolRequest, in groupsIn) (*mcp.CallToolResult, groupsOut, error) {
	all, err := t.lib.CitationGroups(ctx, in.Detail)
	if err != nil {
		return nil, groupsOut{}, toolError(err)
	}
	out := groupsOut{Groups: []CitationGroup{}, Detail: all.Detail, Linked: all.Linked, Fringe: all.Fringe}
	shape := func(i int, g library.CitationGroup, n int) CitationGroup {
		c := CitationGroup{Number: i + 1, Name: g.Name, Anchor: g.Anchor, Papers: len(g.Members), Fetched: g.Fetched,
			YearFrom: g.YearFrom, YearTo: g.YearTo, Top: []GroupPaper{}}
		for _, m := range g.Members[:min(n, len(g.Members))] {
			c.Top = append(c.Top, GroupPaper{FilID: m.FilID, Title: m.Title, FirstAuthor: m.Author, Year: m.Year,
				Stub: !m.Hydrated, CitedWithin: g.CitedWithin[m.FilID]})
		}
		c.More = len(g.Members) - len(c.Top)
		return c
	}
	if strings.TrimSpace(in.Group) == "" {
		for i, g := range all.Groups {
			out.Groups = append(out.Groups, shape(i, g, groupTopPapers))
		}
		if len(out.Groups) == 0 {
			out.Note = fmt.Sprintf("No groups: %d of %d papers are linked to at least two others. expand_graph grows "+
				"the graph; where OpenAlex has few reference lists, groups stay few.", all.Linked, all.Total)
		}
		return nil, out, nil
	}
	limit := in.Limit
	if limit <= 0 {
		limit = defaultGroupPapers
	}
	g, err := t.lib.CitationGroup(ctx, in.Group, in.Detail)
	if errors.Is(err, library.ErrNoGroup) {
		return nil, groupsOut{}, fmt.Errorf("no group %q at %s detail; call citation_groups without group to list them", in.Group, all.Detail)
	}
	if err != nil {
		return nil, groupsOut{}, toolError(err)
	}
	for i, a := range all.Groups {
		if a.Anchor == g.Anchor {
			out.Groups = append(out.Groups, shape(i, g, min(limit, maxGroupPapers)))
		}
	}
	return nil, out, nil
}
