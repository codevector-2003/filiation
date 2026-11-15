// Command retrieval scores how well fil finds the paper that answers a
// question: the test set the product plan's Phase 4 asks for, written before
// the retriever it grades (PRODUCT_PLAN.md, M4).
//
// Each question in questions.json names the paper that answers it, by DOI or
// OpenAlex ID so the set works on any library that holds those papers, and
// the answer as that paper gives it. A method returns passages, best first;
// the papers they come from, in order of first appearance, are the ranking.
// A question is a hit when its paper is in the first three.
//
// The questions are worded as a researcher would ask, not in the passage's
// own words, or keyword search would be graded on copying.
//
// Usage:
//
//	go run ./eval/retrieval -library <folder> [-method keyword] [-v]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/library"
)

// Question is one entry of the test set.
type Question struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Paper    string `json:"paper"`  // DOI or OpenAlex ID of the paper that answers it
	Title    string `json:"title"`  // for people reading the set
	Answer   string `json:"answer"` // as the paper gives it
	Page     int    `json:"page,omitempty"`
}

// method returns the papers, by fil ID, best first, that a way of searching
// finds for a question.
type method func(ctx context.Context, l *library.Library, q string) ([]string, error)

var methods = map[string]method{
	// keyword is whatever fil search does now (D35), so running it before
	// and after a change to the ranking measures the change.
	"keyword": func(ctx context.Context, l *library.Library, q string) ([]string, error) {
		res, err := l.Search(ctx, q, library.SearchOptions{Limit: 30, PerPaper: 3})
		if err != nil {
			return nil, err
		}
		return papersOf(res.Hits), nil
	},
}

func papersOf(hits []library.SearchHit) []string {
	var out []string
	seen := map[string]bool{}
	for _, h := range hits {
		if !seen[h.Work.FilID] {
			seen[h.Work.FilID] = true
			out = append(out, h.Work.FilID)
		}
	}
	return out
}

func main() {
	folder := flag.String("library", "", "the library folder to search")
	name := flag.String("method", "keyword", "how to search")
	set := flag.String("questions", filepath.Join("eval", "retrieval", "questions.json"), "the test set")
	verbose := flag.Bool("v", false, "list the papers each question found")
	flag.Parse()
	if err := run(*folder, *name, *set, *verbose); err != nil {
		fmt.Fprintln(os.Stderr, "retrieval:", err)
		os.Exit(1)
	}
}

func run(folder, name, set string, verbose bool) error {
	m, ok := methods[name]
	if !ok {
		return fmt.Errorf("no method %q", name)
	}
	raw, err := os.ReadFile(set)
	if err != nil {
		return err
	}
	var qs []Question
	if err := json.Unmarshal(raw, &qs); err != nil {
		return fmt.Errorf("%s: %w", set, err)
	}
	ctx := context.Background()
	cfg, err := config.Load(config.Options{LibraryFlag: folder})
	if err != nil {
		return err
	}
	l, err := library.Open(ctx, cfg, library.Options{})
	if err != nil {
		return err
	}
	defer l.Close()

	fmt.Printf("| # | Question | Rank of its paper |\n| --- | --- | --- |\n")
	hits, missing := 0, 0
	var reciprocal float64
	for _, q := range qs {
		want, err := l.Find(ctx, q.Paper)
		if err != nil {
			fmt.Printf("| %s | %s | not in this library |\n", q.ID, q.Question)
			missing++
			continue
		}
		got, err := m(ctx, l, q.Question)
		if err != nil {
			return fmt.Errorf("%s: %w", q.ID, err)
		}
		rank := 0
		for i, id := range got {
			if id == want.FilID {
				rank = i + 1
				break
			}
		}
		shown := "not found"
		if rank > 0 {
			shown = fmt.Sprint(rank)
			reciprocal += 1 / float64(rank)
		}
		if rank > 0 && rank <= 3 {
			hits++
		}
		fmt.Printf("| %s | %s | %s |\n", q.ID, q.Question, shown)
		if verbose {
			for i, id := range got[:min(5, len(got))] {
				w, _ := l.Find(ctx, id)
				fmt.Printf("|  |  %d. %s |  |\n", i+1, strings.TrimSpace(w.DisplayTitle()))
			}
		}
	}
	asked := len(qs) - missing
	fmt.Printf("\n**%s:** paper in the first three for %d of %d questions", name, hits, asked)
	if asked > 0 {
		fmt.Printf("; mean reciprocal rank %.2f", reciprocal/float64(asked))
	}
	if missing > 0 {
		fmt.Printf("; %d questions' papers are not in this library", missing)
	}
	fmt.Println(".")
	return nil
}
