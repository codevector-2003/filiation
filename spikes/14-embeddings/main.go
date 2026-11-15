// Spike 14: can fil find passages by meaning with a model that runs inside
// the binary, in pure Go, with no cgo and no Ollama?
//
// It embeds every passage of an evaluation library (eval/retrieval) with one
// model, then scores the 20 questions by plain vector search over all of
// them: where each question's paper first appears among the papers of the
// nearest passages. That is the score hybrid search must beat later.
//
//	go run . -db <library>/filiation.db -model <dir> [-kind minilm|bge]
//	go run . -db <library>/filiation.db -ollama nomic-embed-text
//
// Embeddings are cached beside the model, so a second run only scores.
package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/pipelines"
	_ "github.com/ncruces/go-sqlite3/driver"
)

var benchN, maxWords, batchSize int

type passage struct {
	id    int64
	paper string // OpenAlex ID
	text  string
}

type question struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Paper    string `json:"paper"`
}

// embedder turns texts into unit vectors. query says whether they are
// questions, for models that word the two differently.
type embedder func(ctx context.Context, texts []string, query bool) ([][]float32, error)

func main() {
	db := flag.String("db", "", "the evaluation library's filiation.db")
	model := flag.String("model", "", "a downloaded ONNX model folder, run in pure Go")
	kind := flag.String("kind", "minilm", "minilm or bge: how the model wants questions worded")
	ollama := flag.String("ollama", "", "an Ollama model to compare against instead")
	set := flag.String("questions", filepath.Join("..", "..", "eval", "retrieval", "questions.json"), "the test set")
	cacheDir := flag.String("cache", "", "where to keep embeddings (default: beside the model)")
	flag.IntVar(&benchN, "bench", 0, "only time embedding this many passages, then stop")
	flag.IntVar(&maxWords, "maxwords", 0, "cut each passage to this many words first")
	flag.IntVar(&batchSize, "batch", 32, "passages per call")
	flag.Parse()
	if err := run(*db, *model, *kind, *ollama, *set, *cacheDir); err != nil {
		fmt.Fprintln(os.Stderr, "spike:", err)
		os.Exit(1)
	}
}

func run(dbPath, model, kind, ollama, set, cacheDir string) error {
	ctx := context.Background()
	ps, err := passages(dbPath)
	if err != nil {
		return err
	}
	fmt.Printf("%d passages (reference lists left out, as fil search does)\n", len(ps))

	var embed embedder
	name := ollama
	switch {
	case ollama != "":
		embed = ollamaEmbedder(ollama)
		if cacheDir == "" {
			cacheDir = "."
		}
	case model != "":
		name = filepath.Base(model)
		start := time.Now()
		session, err := hugot.NewGoSession(ctx)
		if err != nil {
			return err
		}
		defer session.Destroy()
		pipe, err := hugot.NewPipeline(session, hugot.FeatureExtractionConfig{
			ModelPath: model, Name: "embed", OnnxFilename: "model.onnx",
			Options: []hugot.FeatureExtractionOption{pipelines.WithNormalization()},
		})
		if err != nil {
			return err
		}
		fmt.Printf("model loaded in %s\n", time.Since(start).Round(time.Millisecond))
		embed = func(ctx context.Context, texts []string, query bool) ([][]float32, error) {
			if query && kind == "bge" {
				for i, t := range texts {
					texts[i] = "Represent this sentence for searching relevant passages: " + t
				}
			}
			out, err := pipe.RunPipeline(ctx, texts)
			if err != nil {
				return nil, err
			}
			return out.Embeddings, nil
		}
		if cacheDir == "" {
			cacheDir = model
		}
	default:
		return fmt.Errorf("give -model or -ollama")
	}

	if benchN > 0 {
		start := time.Now()
		for i := 0; i < benchN; i += batchSize {
			texts := []string{}
			for _, p := range ps[i:min(i+batchSize, benchN)] {
				texts = append(texts, docText(kind, ollama, p.text))
			}
			if _, err := embed(ctx, texts, false); err != nil {
				return err
			}
		}
		fmt.Printf("bench: %d passages, batch %d, maxwords %d, GOMAXPROCS %d: %.2f passages/s\n",
			benchN, batchSize, maxWords, runtime.GOMAXPROCS(0), float64(benchN)/time.Since(start).Seconds())
		return nil
	}
	cache := filepath.Join(cacheDir, "spike14-"+strings.ReplaceAll(name, ":", "_")+".vec")
	vecs, err := load(cache, len(ps))
	if err != nil {
		var m runtime.MemStats
		start := time.Now()
		vecs = make([][]float32, 0, len(ps))
		batch := batchSize
		for i := 0; i < len(ps); i += batch {
			texts := make([]string, 0, batch)
			for _, p := range ps[i:min(i+batch, len(ps))] {
				texts = append(texts, docText(kind, ollama, p.text))
			}
			v, err := embed(ctx, texts, false)
			for try := 1; err != nil && try <= 5; try++ { // Ollama on Windows can run short of sockets
				time.Sleep(time.Duration(try) * 2 * time.Second)
				v, err = embed(ctx, texts, false)
			}
			if err != nil {
				return fmt.Errorf("passages %d–: %w", i, err)
			}
			vecs = append(vecs, v...)
			if (i/batch)%20 == 0 {
				el := time.Since(start)
				fmt.Printf("  %d of %d, %.1f passages/s\n", len(vecs), len(ps), float64(len(vecs))/el.Seconds())
			}
		}
		el := time.Since(start)
		runtime.ReadMemStats(&m)
		fmt.Printf("embedded %d passages in %s: %.1f passages/s, %d dimensions, heap high water about %d MB\n",
			len(vecs), el.Round(time.Second), float64(len(vecs))/el.Seconds(), len(vecs[0]), m.Sys>>20)
		if err := save(cache, vecs); err != nil {
			return err
		}
	} else {
		fmt.Printf("embeddings read from %s\n", cache)
	}

	raw, err := os.ReadFile(set)
	if err != nil {
		return err
	}
	var qs []question
	if err := json.Unmarshal(raw, &qs); err != nil {
		return err
	}
	hits, recip := 0, 0.0
	var queryTime time.Duration
	fmt.Println("\n| # | rank |\n| --- | --- |")
	for _, q := range qs {
		start := time.Now()
		qv, err := embed(ctx, []string{q.Question}, true)
		if err != nil {
			return err
		}
		ranked := rank(qv[0], vecs, ps)
		queryTime += time.Since(start)
		r := 0
		for i, p := range ranked {
			if p == q.Paper {
				r = i + 1
				break
			}
		}
		shown := "not found"
		if r > 0 {
			shown = fmt.Sprint(r)
			recip += 1 / float64(r)
			if r <= 3 {
				hits++
			}
		}
		fmt.Printf("| %s | %s |\n", q.ID, shown)
	}
	fmt.Printf("\n**%s, vector over every passage:** paper in the first three for %d of %d; mean reciprocal rank %.2f; %s a question (embed and scan)\n",
		name, hits, len(qs), recip/float64(len(qs)), (queryTime / time.Duration(len(qs))).Round(time.Millisecond))
	return nil
}

func docText(kind, ollama, t string) string {
	if maxWords > 0 {
		if w := strings.Fields(t); len(w) > maxWords {
			t = strings.Join(w[:maxWords], " ")
		}
	}
	if strings.HasPrefix(ollama, "nomic") {
		return "search_document: " + t
	}
	return t
}

// rank orders papers by their nearest passage, best first.
func rank(q []float32, vecs [][]float32, ps []passage) []string {
	type scored struct {
		i int
		s float32
	}
	all := make([]scored, len(vecs))
	for i, v := range vecs {
		var dot float32
		for j := range v {
			dot += v[j] * q[j]
		}
		all[i] = scored{i, dot}
	}
	sort.Slice(all, func(a, b int) bool { return all[a].s > all[b].s })
	var out []string
	seen := map[string]bool{}
	for _, s := range all {
		p := ps[s.i].paper
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

func passages(path string) ([]passage, error) {
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT c.id, COALESCE(w.openalex_id, ''), c.text FROM chunk c JOIN work w ON w.fil_id = c.work_id
		WHERE c.section IS NULL OR c.section <> 'references' ORDER BY c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []passage
	for rows.Next() {
		var p passage
		if err := rows.Scan(&p.id, &p.paper, &p.text); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func ollamaEmbedder(model string) embedder {
	return func(ctx context.Context, texts []string, query bool) ([][]float32, error) {
		if query && strings.HasPrefix(model, "nomic") {
			for i, t := range texts {
				texts[i] = "search_query: " + t
			}
		}
		body, _ := json.Marshal(map[string]any{"model": model, "input": texts})
		req, _ := http.NewRequestWithContext(ctx, "POST", "http://127.0.0.1:11434/api/embed", bytes.NewReader(body))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		var out struct {
			Embeddings [][]float32 `json:"embeddings"`
			Error      string      `json:"error"`
		}
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			return nil, err
		}
		if out.Error != "" {
			return nil, fmt.Errorf("ollama: %s", out.Error)
		}
		for _, v := range out.Embeddings { // Ollama normalises, but be sure
			var n float64
			for _, x := range v {
				n += float64(x) * float64(x)
			}
			n = math.Sqrt(n)
			for j := range v {
				v[j] = float32(float64(v[j]) / n)
			}
		}
		return out.Embeddings, nil
	}
}

func save(path string, vecs [][]float32) error {
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, uint32(len(vecs)))
	binary.Write(&b, binary.LittleEndian, uint32(len(vecs[0])))
	for _, v := range vecs {
		binary.Write(&b, binary.LittleEndian, v)
	}
	return os.WriteFile(path, b.Bytes(), 0o644)
}

func load(path string, want int) ([][]float32, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r := bytes.NewReader(raw)
	var n, d uint32
	binary.Read(r, binary.LittleEndian, &n)
	binary.Read(r, binary.LittleEndian, &d)
	if int(n) != want {
		return nil, fmt.Errorf("cache holds %d, want %d", n, want)
	}
	out := make([][]float32, n)
	for i := range out {
		out[i] = make([]float32, d)
		if err := binary.Read(r, binary.LittleEndian, out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}
