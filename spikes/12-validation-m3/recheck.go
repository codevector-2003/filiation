package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/library"
)

// recheckImports runs the import check again on a finished run's import
// libraries, after a fix, without downloading or expanding anything: each
// file is matched by its bytes to the paper it was downloaded for. It
// reports, per field, what was filed and whether rightly, and for the files
// still waiting whether the right paper is among the candidates and whether
// --accept-first would have filed it — and filed it rightly.
func recheckImports(ctx context.Context, root string) {
	cache := filepath.Join(root, "cache")
	fmt.Println("| Field | PDFs | Filed | Filed rightly | Waiting, right paper offered | --accept-first would file | ... rightly | Not offered |")
	fmt.Println("| --- | --- | --- | --- | --- | --- | --- | --- |")
	var notes []string
	for _, f := range fields(seedResults()) {
		want := map[string]string{} // sha256 → OpenAlex ID it was downloaded for
		for i, s := range seeds {
			if s.field != f {
				continue
			}
			lib, err := openAt(ctx, filepath.Join(root, fmt.Sprintf("lib%02d", i)), cache)
			if err != nil {
				fatal(err)
			}
			for path, id := range pdfsOf(ctx, lib) {
				want[hashOf(path)] = id
			}
			lib.Close()
		}

		lib, err := openAt(ctx, filepath.Join(root, "import-"+slug(f)), cache)
		if err != nil {
			fatal(err)
		}
		total, filed, rightly, offered, accept, acceptRight, missing := 0, 0, 0, 0, 0, 0, 0
		for path, id := range pdfsOf(ctx, lib) {
			total++
			filed++
			if want[hashOf(path)] == id {
				rightly++
			} else {
				notes = append(notes, fmt.Sprintf("%s: %s filed as %s, downloaded for %s", f, filepath.Base(path), id, want[hashOf(path)]))
			}
		}
		res, err := lib.Import(ctx, library.ImportOptions{Again: true})
		if err != nil {
			fatal(fmt.Errorf("%s: import: %w", f, err))
		}
		for _, file := range res.Files {
			total++
			id := want[hashOf(file.File)]
			if file.Outcome == library.ImportImported {
				filed++
				if file.Work.OpenAlexID == id {
					rightly++
				}
				continue
			}
			found := false
			for _, c := range file.Candidates {
				found = found || c.Work.OpenAlexID == id
			}
			if found {
				offered++
			} else {
				missing++
				notes = append(notes, fmt.Sprintf("%s: %s (%s) %s — %s", f, filepath.Base(file.File), id, file.Outcome, file.Reason))
			}
			if len(file.Candidates) > 0 && file.Candidates[0].Verified && file.Outcome == library.ImportCandidates {
				accept++
				if file.Candidates[0].Work.OpenAlexID == id {
					acceptRight++
				} else {
					notes = append(notes, fmt.Sprintf("%s: %s --accept-first would file %s, downloaded for %s", f, filepath.Base(file.File), file.Candidates[0].Work.OpenAlexID, id))
				}
			}
		}
		lib.Close()
		fmt.Printf("| %s | %d | %d | %d | %d | %d | %d | %d |\n", f, total, filed, rightly, offered, accept, acceptRight, missing)
	}
	fmt.Println()
	for _, n := range notes {
		fmt.Println("- " + n)
	}
}

func seedResults() []result {
	out := make([]result, len(seeds))
	for i, s := range seeds {
		out[i].seed = s
	}
	return out
}

func openAt(ctx context.Context, dir, cache string) (*library.Library, error) {
	cfg := &config.Config{MaxNodes: maxNodes, MaxDepth: config.DefaultMaxDepth,
		MaxRefsPerWork: config.DefaultMaxRefsPerWork, FolderCollections: true}
	if err := cfg.UseFolder(dir); err != nil {
		return nil, err
	}
	return library.Open(ctx, cfg, library.Options{CacheDir: cache})
}

// pdfsOf maps each PDF on disk in a library to the OpenAlex ID of its paper.
func pdfsOf(ctx context.Context, lib *library.Library) map[string]string {
	rows, err := lib.List(ctx, library.ListOptions{WithPDF: true})
	if err != nil {
		fatal(err)
	}
	out := map[string]string{}
	for _, r := range rows {
		p, err := lib.Show(ctx, r.FilID)
		if err != nil {
			fatal(err)
		}
		for _, f := range p.Files {
			if !f.Missing {
				out[f.Path] = r.OpenAlexID
			}
		}
	}
	return out
}

func hashOf(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
