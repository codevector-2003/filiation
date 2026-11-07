package graph

import (
	"cmp"
	"math/rand/v2"
	"slices"

	"gonum.org/v1/gonum/graph/community"
	"gonum.org/v1/gonum/graph/simple"
)

// MinGroup is the fewest papers a citation group is suggested with. A smaller
// cluster is a paper and a couple of its references, not a topic.
const MinGroup = 5

// Group is one cluster of papers that cite one another more than they cite
// the rest of the library (D34).
type Group struct {
	// Anchor is the member the others cite most: the paper the group is
	// around, and the key a choice about the group is kept under.
	Anchor string
	// Members are the papers in the group, most cited by the others first.
	Members []string
	// CitedWithin counts, for each member, the other members citing it.
	CitedWithin map[string]int
	// Fringe are the papers linked to this group and to nothing else in the
	// library: left out of the grouping, since one link says little.
	Fringe []string
}

// Grouping is every group found, and what was left out.
type Grouping struct {
	Groups []Group // largest first
	Linked int     // papers linked to at least two others, which were grouped
	Small  int     // of those, in a cluster smaller than MinGroup
	Fringe int     // papers linked to exactly one other
}

// FindGroups clusters the citation graph into groups by Louvain modularity
// over the citations as undirected links, at the given resolution: higher
// gives more, smaller groups. Only papers linked to at least two others are
// grouped; a reference one paper cites tells nothing about where it belongs,
// and in a library grown by expansion such references are most of it.
//
// The same citations always give the same groups: papers are numbered in
// fil ID order and the search is seeded with a constant.
func FindGroups(citations [][2]string, resolution float64) Grouping {
	links := map[string]map[string]bool{}
	link := func(a, b string) {
		if links[a] == nil {
			links[a] = map[string]bool{}
		}
		links[a][b] = true
	}
	for _, c := range citations {
		if c[0] != c[1] {
			link(c[0], c[1])
			link(c[1], c[0])
		}
	}
	var linked []string
	fringe := 0
	for id, ns := range links {
		if len(ns) >= 2 {
			linked = append(linked, id)
		} else {
			fringe++
		}
	}
	slices.Sort(linked)
	number := make(map[string]int64, len(linked))
	for i, id := range linked {
		number[id] = int64(i)
	}

	g := simple.NewUndirectedGraph()
	for i := range linked {
		g.AddNode(simple.Node(int64(i)))
	}
	for _, id := range linked {
		for n := range links[id] {
			if j, ok := number[n]; ok && number[id] < j {
				g.SetEdge(simple.Edge{F: simple.Node(number[id]), T: simple.Node(j)})
			}
		}
	}
	out := Grouping{Linked: len(linked), Fringe: fringe}
	if len(linked) == 0 {
		return out
	}

	reduced := community.Modularize(g, resolution, rand.NewPCG(1, 2))
	groupOf := map[string]int{}
	for _, c := range reduced.Communities() {
		if len(c) < MinGroup {
			out.Small += len(c)
			continue
		}
		grp := Group{CitedWithin: make(map[string]int, len(c))}
		for _, n := range c {
			id := linked[n.ID()]
			grp.Members = append(grp.Members, id)
			grp.CitedWithin[id] = 0
			groupOf[id] = len(out.Groups)
		}
		out.Groups = append(out.Groups, grp)
	}
	for _, c := range citations {
		if c[0] == c[1] {
			continue
		}
		if a, ok := groupOf[c[0]]; ok {
			if b, ok := groupOf[c[1]]; ok && a == b {
				out.Groups[a].CitedWithin[c[1]]++
			}
		}
	}
	for id, ns := range links {
		if len(ns) != 1 {
			continue
		}
		for n := range ns {
			if i, ok := groupOf[n]; ok {
				out.Groups[i].Fringe = append(out.Groups[i].Fringe, id)
			}
		}
	}
	for i := range out.Groups {
		grp := &out.Groups[i]
		slices.SortFunc(grp.Members, func(a, b string) int {
			return cmp.Or(cmp.Compare(grp.CitedWithin[b], grp.CitedWithin[a]), cmp.Compare(a, b))
		})
		slices.Sort(grp.Fringe)
		grp.Anchor = grp.Members[0]
	}
	slices.SortFunc(out.Groups, func(a, b Group) int {
		return cmp.Or(cmp.Compare(len(b.Members), len(a.Members)), cmp.Compare(a.Anchor, b.Anchor))
	})
	return out
}
