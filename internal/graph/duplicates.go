package graph

import (
	"context"
	"sort"

	"github.com/codevector-2003/filiation/internal/identity"
	"github.com/codevector-2003/filiation/internal/model"
)

// minDuplicateKey is the shortest normalised title considered for a duplicate
// report. Below it sit generic titles — "Editorial", "Introduction",
// "Preface", "Book reviews" — shared by thousands of unrelated works; listing
// them would bury the real cases in noise.
const minDuplicateKey = 20

// DuplicateGroup is a set of fetched works with the same title: probably one
// paper that OpenAlex holds as more than one record, under different DOIs or
// none.
type DuplicateGroup struct {
	Title string       // as the first work in the group spells it
	Works []model.Work // two or more, ordered by year, then ID
}

// ProbableDuplicates lists groups of fetched works whose titles match once
// case, punctuation and spacing are ignored.
//
// It reports and never merges (D14). A matching title is evidence, not proof:
// a book review routinely carries the title of the book it reviews, and a
// preprint and its journal version are one work to a reader but two records
// that may differ. Duplicates that are certain — the same OpenAlex ID, a merged
// record, the same DOI — are already folded on the way in (D2, graph.hydrate);
// what is left here is for a person to judge.
func (g *Graph) ProbableDuplicates(ctx context.Context) ([]DuplicateGroup, error) {
	works, err := g.db.TitledWorks(ctx)
	if err != nil {
		return nil, err
	}

	byKey := map[string][]model.Work{}
	for _, w := range works {
		key := identity.TitleKey(*w.Title)
		if len([]rune(key)) < minDuplicateKey {
			continue
		}
		byKey[key] = append(byKey[key], w)
	}

	var groups []DuplicateGroup
	for _, ws := range byKey {
		if len(ws) < 2 {
			continue
		}
		sort.Slice(ws, func(i, j int) bool {
			yi, yj := model.Deref(ws[i].Year, 0), model.Deref(ws[j].Year, 0)
			if yi != yj {
				return yi < yj
			}
			return ws[i].OpenAlexID < ws[j].OpenAlexID
		})
		groups = append(groups, DuplicateGroup{Title: *ws[0].Title, Works: ws})
	}
	// Largest groups first, then alphabetical, so the list is stable between
	// runs and the worst cases lead.
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].Works) != len(groups[j].Works) {
			return len(groups[i].Works) > len(groups[j].Works)
		}
		return groups[i].Title < groups[j].Title
	})
	return groups, nil
}
