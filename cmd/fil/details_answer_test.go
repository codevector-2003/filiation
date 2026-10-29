package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	batchByID = regexp.MustCompile(`^/works\?filter=openalex_id:(W\d+(?:\|W\d+)*)$`)
	singleID  = regexp.MustCompile(`^/works/W\d+$`)
)

// detailsAnswer answers the lookups reading a PDF makes for its references'
// details (graph.DetailIDs) when no response was recorded for that exact
// batch: the requested works found in the recorded batch of the M0 seed's
// references, and "not found" for any single lookup. Only replays that ask
// for it use it, so an unexpected request still fails every other test.
func detailsAnswer(key string) (int, string, bool) {
	if singleID.MatchString(key) {
		return 404, "", true
	}
	m := batchByID.FindStringSubmatch(key)
	if m == nil {
		return 0, "", false
	}
	want := map[string]bool{}
	for _, id := range strings.Split(m[1], "|") {
		want[id] = true
	}
	raw, err := os.ReadFile(filepath.Join(fixtureDir, "batch_peerj_refs.json"))
	if err != nil {
		return 0, "", false
	}
	var all struct {
		Results []map[string]any `json:"results"`
	}
	if json.Unmarshal(raw, &all) != nil {
		return 0, "", false
	}
	var keep []map[string]any
	for _, w := range all.Results {
		id, _ := w["id"].(string)
		if want[id[strings.LastIndexByte(id, '/')+1:]] {
			keep = append(keep, w)
		}
	}
	body, _ := json.Marshal(map[string]any{"meta": map[string]any{"count": len(keep)}, "results": keep})
	return 200, string(body), true
}
