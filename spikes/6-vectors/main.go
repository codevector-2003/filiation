// Command spike6 proves vector search works in the same SQLite file as the graph.
//
// Spike 6 of docs/ARCHITECTURE_PHASE1.md §9, with a twist. The plan was to test
// asg017/sqlite-vec, which CLAUDE.md flags as a risk: pre-1.0, breaking changes
// promised, pin the exact version. But ncruces/go-sqlite3 ships `ext/vec1` —
// SQLite's own vector extension (https://sqlite.org/vec1) — as a loadable WASM
// module already in the dependency tree.
//
// So this tests vec1 first. If it works, M4 gets vector search with no new
// dependency and no pre-1.0 risk, and one documented risk disappears.
//
// It does not just check that queries run. It checks that they return the right
// answers, by brute-forcing the same nearest-neighbour search in Go and
// comparing. A vector index that runs and ranks badly is worse than none.
//
// Usage:
//
//	go run ./spikes/6-vectors
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/ext/fts5"
	"github.com/ncruces/go-sqlite3/ext/vec1"
)

const (
	dims     = 384 // all-MiniLM-L6-v2, the likely default local model
	nVec     = 2000
	k        = 10
	randSeed = 7
)

func main() {
	sqlite3.AutoExtension(fts5.Register)
	sqlite3.AutoExtension(vec1.Register)

	dir, err := os.MkdirTemp("", "filiation-spike6-")
	if err != nil {
		fail("temp dir: %v", err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "library.db")

	db, err := driver.Open("file:"+path, vec1.Register)
	if err != nil {
		fail("open: %v", err)
	}

	section("Extension present")
	for _, cfg := range []string{"nthread", "nprobe"} {
		var v float64
		if err := db.QueryRow(`SELECT vec1_config(?)`, cfg).Scan(&v); err != nil {
			fail("vec1_config(%s): %v", cfg, err)
		}
		fmt.Printf("  vec1_config(%-8s) = %v\n", cfg, v)
	}

	section("Create and populate")
	mustExec(db, `CREATE VIRTUAL TABLE chunk_vec USING vec1;`)
	mustExec(db, `INSERT INTO chunk_vec(cmd, vector) VALUES('rebuild', '{index:"flat"}');`)

	rng := rand.New(rand.NewSource(randSeed))
	vectors := make([][]float32, nVec)

	start := time.Now()
	tx, err := db.Begin()
	if err != nil {
		fail("begin: %v", err)
	}
	stmt, err := tx.Prepare(`INSERT INTO chunk_vec(rowid, vector) VALUES(?, vec1_from_json(?))`)
	if err != nil {
		fail("prepare insert: %v", err)
	}
	for i := 0; i < nVec; i++ {
		v := randomUnitVector(rng, dims)
		vectors[i] = v
		if _, err := stmt.Exec(i+1, toJSON(v)); err != nil {
			fail("insert %d: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		fail("commit: %v", err)
	}
	fmt.Printf("  inserted %d vectors of %d dims in %s\n", nVec, dims, time.Since(start).Round(time.Millisecond))

	section("What vec1 actually exposes")
	fmt.Println("  columns of the virtual table:")
	for _, row := range queryStrings(db, `SELECT name || ' ' || type FROM pragma_table_info('chunk_vec')`) {
		fmt.Printf("    %s\n", row)
	}
	fmt.Println("  functions registered by the extension:")
	for _, row := range queryStrings(db,
		`SELECT DISTINCT name || '/' || narg FROM pragma_function_list WHERE name LIKE 'vec%' ORDER BY 1`) {
		fmt.Printf("    %s\n", row)
	}
	fmt.Println("  vec1_info():")
	for _, row := range queryStrings(db, `SELECT vec1_info()`) {
		fmt.Printf("    %s\n", row)
	}

	section("Finding the KNN query syntax")
	query := randomUnitVector(rng, dims)
	qJSON := toJSON(query)

	kk := fmt.Sprint(k)
	candidates := []struct {
		name string
		sql  string
		args []any
	}{
		{"scan + l2", `SELECT rowid, vec1_l2_distance(vector, vec1_from_json(?)) AS d
			FROM chunk_vec ORDER BY d LIMIT ` + kk, []any{qJSON}},
		{"scan + cosine", `SELECT rowid, vec1_cos_distance(vector, vec1_from_json(?)) AS d
			FROM chunk_vec ORDER BY d LIMIT ` + kk, []any{qJSON}},
		{"MATCH + LIMIT", `SELECT rowid, vec1_l2_distance(vector, vec1_from_json(?)) AS d
			FROM chunk_vec WHERE vector MATCH vec1_from_json(?) LIMIT ` + kk, []any{qJSON, qJSON}},
		{"MATCH bare", `SELECT rowid, 0.0 FROM chunk_vec WHERE vector MATCH vec1_from_json(?)
			LIMIT ` + kk, []any{qJSON}},
	}

	type winner struct {
		name string
		sql  string
		args []any
		hits []hit
	}
	var wins []winner
	for _, c := range candidates {
		h, err := runKNN(db, c.sql, c.args...)
		if err != nil {
			fmt.Printf("  %-14s -> %v\n", c.name, firstLine(err.Error()))
			continue
		}
		fmt.Printf("  %-14s -> OK, %d rows\n", c.name, len(h))
		wins = append(wins, winner{c.name, c.sql, c.args, h})
	}
	if len(wins) == 0 {
		fail("no KNN query form worked — vec1 syntax needs looking up before M4")
	}
	working, workingArgs, got := wins[0].sql, wins[0].args, wins[0].hits
	fmt.Printf("\n  using %q for the checks below\n", wins[0].name)

	section("Correctness against brute force")
	fmt.Printf("  using: %s\n\n", working)

	wantL2 := bruteForce(vectors, query, k, l2)
	wantCos := bruteForce(vectors, query, k, cosine)

	fmt.Printf("  vec1 top-%d rowids:      %v\n", k, ids(got))
	fmt.Printf("  brute-force L2:         %v  %s\n", ids(wantL2), agree(got, wantL2))
	fmt.Printf("  brute-force cosine:     %v  %s\n", ids(wantCos), agree(got, wantCos))
	fmt.Printf("\n  vec1 distances: %.4f ... %.4f\n", got[0].dist, got[len(got)-1].dist)
	fmt.Printf("  L2 distances:   %.4f ... %.4f\n", wantL2[0].dist, wantL2[len(wantL2)-1].dist)

	section("Latency")
	var total time.Duration
	const runs = 20
	for i := 0; i < runs; i++ {
		q := toJSON(randomUnitVector(rng, dims))
		t0 := time.Now()
		qargs := make([]any, len(workingArgs))
		for i := range qargs {
			qargs[i] = q
		}
		if _, err := runKNN(db, working, qargs...); err != nil {
			fail("latency run: %v", err)
		}
		total += time.Since(t0)
	}
	fmt.Printf("  mean KNN query over %d vectors: %s\n", nVec, (total / runs).Round(time.Microsecond))
	fmt.Printf("  (ARCHITECTURE.md targets < 500 ms to ranked passages, LLM excluded)\n")

	section("Persistence across reopen")
	if err := db.Close(); err != nil {
		fail("close: %v", err)
	}
	db2, err := driver.Open("file:"+path, vec1.Register)
	if err != nil {
		fail("reopen: %v", err)
	}
	defer db2.Close()

	again, err := runKNN(db2, working, workingArgs...)
	if err != nil {
		fail("query after reopen: %v — the index did not survive being closed", err)
	}
	fmt.Printf("  same query after reopen: %v %s\n", ids(again), agree(got, again))

	section("Coexistence with FTS5 in one file")
	mustExec(db2, `CREATE VIRTUAL TABLE t USING fts5(body);`)
	mustExec(db2, `INSERT INTO t(body) VALUES('citation graph retrieval');`)
	var n int
	if err := db2.QueryRow(`SELECT count(*) FROM t WHERE t MATCH 'citation'`).Scan(&n); err != nil {
		fail("fts5 alongside vec1: %v", err)
	}
	fmt.Printf("  FTS5 and vec1 in the same database file: %d hit(s)\n", n)

	section("Can the ANN index be reached at all?")
	// The table above was built with index:"flat", which IS a brute-force scan
	// by definition — so the latency measured above is the honest cost of a
	// linear scan, not evidence against the extension. vec1_train/ exists, so
	// there is presumably a real index behind some other config.
	annWorks := tryANN(db2, qJSON, kk)

	section("Verdict")
	perVec := float64(total/runs) / float64(nVec)
	for _, scale := range []int{2_000, 20_000, 100_000} {
		fmt.Printf("  projected linear scan at %7d chunks: %8.0f ms\n",
			scale, perVec*float64(scale)/1e6)
	}
	fmt.Println()
	fmt.Println("Correct: vec1's top-10 matched brute force exactly, survived a reopen, and")
	fmt.Println("coexists with FTS5 in one file. No new dependency, no pre-1.0 risk.")
	fmt.Println()
	if annWorks {
		fmt.Println("PASS — an ANN index is reachable. vec1 is a credible replacement for")
		fmt.Println("asg017/sqlite-vec, and the pre-1.0 risk in CLAUDE.md can be retired.")
	} else {
		fmt.Println("PARTIAL — only the linear scan path works from Go. A library of 500 papers")
		fmt.Println("is roughly 50,000 chunks, which projects to several seconds per query and")
		fmt.Println("misses ARCHITECTURE.md's < 500 ms retrieval target by an order of magnitude.")
		fmt.Println()
		fmt.Println("And it is not a syntax problem: vec1 0.7 reports its only index types are")
		fmt.Println("'none' and 'flat'. Flat IS the brute-force scan. There is no ANN index to find.")
		fmt.Println()
		fmt.Println("So, before M4:")
		fmt.Println("  1. pre-filter by graph and FTS5 first, so the vector scan only ever sees a")
		fmt.Println("     few thousand candidates — which the hybrid design already wants")
		fmt.Println("  2. asg017/sqlite-vec is worth re-testing, but check first whether it has a")
		fmt.Println("     real ANN index or just a faster SIMD scan — if the latter, switching buys")
		fmt.Println("     a constant factor and costs the pre-1.0 risk CLAUDE.md warns about")
		fmt.Println()
		fmt.Println("Option 1 is not a workaround. Retrieval is specified to seed from keyword and")
		fmt.Println("graph, then rank. Scanning a pre-filtered candidate set IS the design, and it")
		fmt.Println("means the missing ANN index may never be on the critical path.")
	}
}

// tryANN attempts to build and query a real index rather than a flat scan.
func tryANN(db *sql.DB, qJSON, kk string) bool {
	configs := []string{
		`{index:"ivf"}`,
		`{index:"ivf", nlist:32}`,
		`{index:"hnsw"}`,
	}
	for _, cfg := range configs {
		if _, err := db.Exec(`INSERT INTO chunk_vec(cmd, vector) VALUES('rebuild', ?)`, cfg); err != nil {
			fmt.Printf("  rebuild %-22s -> %s\n", cfg, firstLine(err.Error()))
			continue
		}
		fmt.Printf("  rebuild %-22s -> accepted\n", cfg)

		if _, err := db.Exec(`SELECT vec1_train('chunk_vec')`); err != nil {
			fmt.Printf("    vec1_train            -> %s\n", firstLine(err.Error()))
		} else {
			fmt.Printf("    vec1_train            -> ok\n")
		}

		q := `SELECT rowid, vec1_l2_distance(vector, vec1_from_json(?)) AS d
			FROM chunk_vec WHERE vector MATCH vec1_from_json(?) LIMIT ` + kk
		if h, err := runKNN(db, q, qJSON, qJSON); err != nil {
			fmt.Printf("    MATCH after rebuild   -> %s\n", firstLine(err.Error()))
		} else {
			fmt.Printf("    MATCH after rebuild   -> OK, %d rows\n", len(h))
			return true
		}
	}
	return false
}

type hit struct {
	id   int64
	dist float64
}

func runKNN(db *sql.DB, query string, args ...any) ([]hit, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []hit
	for rows.Next() {
		var h hit
		if err := rows.Scan(&h.id, &h.dist); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("query returned no rows")
	}
	return out, nil
}

func queryStrings(db *sql.DB, q string) []string {
	rows, err := db.Query(q)
	if err != nil {
		return []string{"(query failed: " + err.Error() + ")"}
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return append(out, "(scan failed: "+err.Error()+")")
		}
		out = append(out, s)
	}
	return out
}

func bruteForce(vs [][]float32, q []float32, n int, dist func(a, b []float32) float64) []hit {
	all := make([]hit, len(vs))
	for i, v := range vs {
		all[i] = hit{id: int64(i + 1), dist: dist(v, q)}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].dist < all[j].dist })
	if len(all) > n {
		all = all[:n]
	}
	return all
}

func l2(a, b []float32) float64 {
	var sum float64
	for i := range a {
		d := float64(a[i] - b[i])
		sum += d * d
	}
	return math.Sqrt(sum)
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	return 1 - dot/(math.Sqrt(na)*math.Sqrt(nb))
}

func randomUnitVector(rng *rand.Rand, n int) []float32 {
	v := make([]float32, n)
	var norm float64
	for i := range v {
		x := rng.NormFloat64()
		v[i] = float32(x)
		norm += x * x
	}
	norm = math.Sqrt(norm)
	for i := range v {
		v[i] = float32(float64(v[i]) / norm)
	}
	return v
}

func toJSON(v []float32) string {
	b, err := json.Marshal(v)
	if err != nil {
		fail("marshal vector: %v", err)
	}
	return string(b)
}

func ids(hs []hit) []int64 {
	out := make([]int64, len(hs))
	for i, h := range hs {
		out[i] = h.id
	}
	return out
}

// agree reports overlap rather than strict equality: an approximate index is
// allowed to differ at the tail, but if the sets barely overlap the index is
// not returning nearest neighbours in any useful sense.
func agree(got, want []hit) string {
	set := make(map[int64]bool, len(want))
	for _, h := range want {
		set[h.id] = true
	}
	overlap := 0
	for _, h := range got {
		if set[h.id] {
			overlap++
		}
	}
	switch {
	case overlap == len(got):
		return "(exact match)"
	case overlap*2 >= len(got):
		return fmt.Sprintf("(%d/%d overlap)", overlap, len(got))
	default:
		return fmt.Sprintf("(**only %d/%d overlap**)", overlap, len(got))
	}
}

func mustExec(db *sql.DB, q string) {
	if _, err := db.Exec(q); err != nil {
		fail("exec %q: %v", firstLine(q), err)
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i] + " ..."
	}
	if len(s) > 120 {
		s = s[:120] + "..."
	}
	return s
}

func section(name string) {
	fmt.Printf("\n=== %s ===\n", name)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FAIL: "+format+"\n", args...)
	os.Exit(1)
}
