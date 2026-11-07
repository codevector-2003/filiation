package graph

import (
	"fmt"
	"reflect"
	"slices"
	"testing"
)

// twoTopics is two tight clusters of six papers each, joined by a single
// citation, with a reference that only one paper of the first cites.
func twoTopics() [][2]string {
	var c [][2]string
	clique := func(prefix string) {
		for i := range 6 {
			for j := range 6 {
				if i > j {
					c = append(c, [2]string{fmt.Sprintf("%s%d", prefix, i), fmt.Sprintf("%s%d", prefix, j)})
				}
			}
		}
	}
	clique("A")
	clique("B")
	// A0 is cited by every other A and by one B: the anchor of A.
	c = append(c, [2]string{"B5", "A0"}, [2]string{"A3", "LEAF"}, [2]string{"A1", "A1"})
	return c
}

func TestFindGroupsSeparatesTopics(t *testing.T) {
	t.Parallel()
	got := FindGroups(twoTopics(), 1)
	if len(got.Groups) != 2 {
		t.Fatalf("groups = %+v; want two", got.Groups)
	}
	if got.Linked != 12 || got.Fringe != 1 || got.Small != 0 {
		t.Errorf("linked %d, fringe %d, small %d; want 12, 1, 0", got.Linked, got.Fringe, got.Small)
	}
	for _, g := range got.Groups {
		if len(g.Members) != 6 || g.Members[0] != g.Anchor {
			t.Errorf("group %+v", g)
		}
		first := g.Anchor[:1]
		for _, m := range g.Members {
			if m[:1] != first {
				t.Errorf("group around %s mixes topics: %v", g.Anchor, g.Members)
			}
		}
		if first == "A" {
			if g.Anchor != "A0" || g.CitedWithin["A0"] != 5 {
				t.Errorf("A's anchor = %s cited %d times within", g.Anchor, g.CitedWithin[g.Anchor])
			}
			if !reflect.DeepEqual(g.Fringe, []string{"LEAF"}) {
				t.Errorf("A's fringe = %v", g.Fringe)
			}
		}
	}
}

func TestFindGroupsIsTheSameEveryTime(t *testing.T) {
	t.Parallel()
	c := twoTopics()
	want := FindGroups(c, 1)
	for range 20 {
		slices.Reverse(c)
		if got := FindGroups(c, 1); !reflect.DeepEqual(got, want) {
			t.Fatalf("a second run differs:\n%+v\n%+v", got, want)
		}
	}
}

func TestFindGroupsLeavesOutSmallClusters(t *testing.T) {
	t.Parallel()
	got := FindGroups([][2]string{{"A", "B"}, {"B", "C"}, {"C", "A"}, {"D", "E"}}, 1)
	if len(got.Groups) != 0 || got.Small != 3 || got.Fringe != 2 {
		t.Fatalf("= %+v; want no group, 3 papers in a small cluster and 2 on the fringe", got)
	}
	if got := FindGroups(nil, 1); len(got.Groups) != 0 || got.Linked != 0 {
		t.Fatalf("no citations = %+v", got)
	}
}
