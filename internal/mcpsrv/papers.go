package mcpsrv

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/model"
)

// Limits for the PDF tools.
const (
	defaultSearchPassages = 10
	maxSearchPassages     = 50
	defaultFetchPDFs      = 20
	maxFetchPDFs          = 100
	maxSentences          = 3 // citation sentences per reference in neighbours
)

// addPaperTools registers the tools over the library's PDFs (M3): searching
// their text, fetching more, and everything about one paper.
func addPaperTools(s *mcp.Server, t tools, readOnly, grows func(string) *mcp.ToolAnnotations) {
	mcp.AddTool(s, &mcp.Tool{
		Name:  "search_library",
		Title: "Search the library's papers",
		Description: "Search the full text of the PDFs in the library by keyword and get the matching " +
			"passages, each with its paper and page. Every word must appear; \"quoted phrases\" match " +
			"as written; word* matches a prefix; words match by stem. If no passage has every word, " +
			"the passages with some of them come back, marked partial. Reference lists are left out " +
			"unless include_references. Only papers whose PDF fil has read are searched: quote the " +
			"passage and its page rather than paraphrasing from memory.",
		Annotations: readOnly("Search the library's papers"),
	}, t.searchLibrary)

	mcp.AddTool(s, &mcp.Tool{
		Name:  "fetch_pdfs",
		Title: "Fetch open-access PDFs",
		Description: "Download the legal open-access PDFs of the library's papers — the user's own " +
			"papers first, then the most cited — into the library folder, and read their text so " +
			"search_library finds them. Never from behind a paywall. Bounded by max_pdfs; call again " +
			"to continue. Give identifier to fetch one paper. Most papers have no open PDF: about one " +
			"in eight overall, nearly half in physics and almost none in the humanities.",
		Annotations: grows("Fetch open-access PDFs"),
	}, t.fetchPDFs)

	mcp.AddTool(s, &mcp.Tool{
		Name:  "get_paper",
		Title: "Everything about a paper",
		Description: "Everything the library knows about one paper: its details, its place in the " +
			"graph, its PDF or why it has none, its text or why it has none, and sentences in which " +
			"it cites its references.",
		Annotations: readOnly("Everything about a paper"),
	}, t.getPaper)
}

// ---- search_library ----

type searchIn struct {
	Query             string `json:"query" jsonschema:"words to find; \"quoted phrases\" match as written"`
	Paper             string `json:"paper,omitempty" jsonschema:"search only this paper: its fil_id, as every passage and paper returned carries (or a DOI, OpenAlex ID, or its exact title)"`
	Limit             int    `json:"limit,omitempty" jsonschema:"how many passages (default 10, at most 50)"`
	IncludeReferences bool   `json:"include_references,omitempty" jsonschema:"search reference lists too"`
}

// Passage is one passage that matched.
type Passage struct {
	Paper   Work   `json:"paper"`
	Page    int    `json:"page" jsonschema:"the page of the PDF it is on, from 1"`
	Text    string `json:"text" jsonschema:"the matching stretch of the passage; matched words are between [[ and ]]"`
	InRefs  bool   `json:"in_reference_list,omitempty" jsonschema:"the passage is from the paper's reference list"`
	PDFPath string `json:"pdf_path" jsonschema:"the PDF on the user's disk"`
}

type searchOut struct {
	Passages []Passage `json:"passages"`
	Partial  bool      `json:"partial,omitempty" jsonschema:"no passage had every word: these have some of them"`
	Unread   int       `json:"unread_pdfs,omitempty" jsonschema:"PDFs in the library not read yet, so not searched"`
	Note     string    `json:"note,omitempty"`
}

func (t tools) searchLibrary(ctx context.Context, _ *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, searchOut, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = defaultSearchPassages
	}
	res, err := t.lib.Search(ctx, in.Query, library.SearchOptions{Limit: min(limit, maxSearchPassages),
		Paper: in.Paper, References: in.IncludeReferences})
	if errors.Is(err, errs.ErrEmptyQuery) {
		return nil, searchOut{}, errors.New("give at least one word to search for")
	}
	if err != nil {
		return nil, searchOut{}, toolError(err)
	}
	out := searchOut{Passages: []Passage{}, Partial: res.Partial, Unread: res.Unread}
	marks := strings.NewReplacer(library.MarkStart, "[[", library.MarkEnd, "]]")
	for _, h := range res.Hits {
		out.Passages = append(out.Passages, Passage{Paper: toWork(h.Work), Page: h.Page,
			Text: strings.Join(strings.Fields(marks.Replace(h.Snippet)), " "), InRefs: h.Section != "", PDFPath: h.File})
	}
	switch {
	case len(out.Passages) == 0 && res.Unread > 0:
		out.Note = fmt.Sprintf("Nothing found. %d PDFs have not been read yet; fetch_pdfs reads them.", res.Unread)
	case len(out.Passages) == 0:
		out.Note = "Nothing found in the PDFs the library has read. Most papers have no open PDF; fetch_pdfs gets those that do."
	case res.Partial:
		out.Note = "No passage has every word; these have some of them, those with the most first."
	}
	return nil, out, nil
}

// ---- fetch_pdfs ----

type fetchIn struct {
	MaxPDFs    int    `json:"max_pdfs,omitempty" jsonschema:"most PDFs to download in this call (default 20, at most 100)"`
	Identifier string `json:"identifier,omitempty" jsonschema:"fetch only this paper, already in the library"`
}

type fetchOut struct {
	Tried      int            `json:"tried" jsonschema:"papers a download was tried for"`
	Downloaded int            `json:"downloaded"`
	Failed     map[string]int `json:"failed_links,omitempty" jsonschema:"links that failed, by why: refused, not-a-pdf, not-found, …"`
	Read       int            `json:"read" jsonschema:"PDFs whose text was read"`
	Searchable int            `json:"searchable" jsonschema:"of those, how many gave searchable text"`
	WithPDF    int            `json:"papers_with_pdf" jsonschema:"papers in the library with a PDF on disk, after this call"`
	Paper      *Work          `json:"paper,omitempty"`
	PDFPath    string         `json:"pdf_path,omitempty"`
	Note       string         `json:"note,omitempty"`
}

func (t tools) fetchPDFs(ctx context.Context, req *mcp.CallToolRequest, in fetchIn) (*mcp.CallToolResult, fetchOut, error) {
	var out fetchOut
	if in.Identifier != "" {
		p, err := t.lib.FetchOne(ctx, in.Identifier)
		if err != nil {
			return nil, fetchOut{}, fetchError(err)
		}
		w := toWork(p.Work)
		out.Paper, out.PDFPath, out.Tried = &w, p.File, 1
		switch {
		case p.Already:
			out.Note = "The PDF was already in the library."
		case p.File != "":
			out.Downloaded = 1
		case len(p.Attempts) == 0:
			out.Note = "No legal PDF link is known for this paper."
		default:
			out.Failed = map[string]int{}
			for _, a := range p.Attempts {
				out.Failed[a.Outcome]++
			}
			out.Note = "Every link failed; the publisher may refuse automated downloads. A link that failed is tried again in a month."
		}
		if p.Text != nil {
			out.Read = 1
			if p.Text.Status == "ok" {
				out.Searchable = 1
			}
		}
		out.WithPDF, _ = t.lib.FetchedOnDisk(ctx)
		return nil, out, nil
	}

	max := in.MaxPDFs
	if max <= 0 {
		max = defaultFetchPDFs
	}
	max = min(max, maxFetchPDFs)
	opts := library.FetchOptions{Max: max}
	if token := req.Params.GetProgressToken(); token != nil {
		opts.Progress = func(f library.Fetched) {
			_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{ProgressToken: token,
				Progress: float64(f.Tried), Total: float64(max),
				Message: fmt.Sprintf("tried %d of up to %d papers, %d downloaded", f.Tried, max, f.Downloaded)})
		}
	}
	res, err := t.lib.Fetch(ctx, opts)
	out.Tried, out.Downloaded, out.Read, out.Searchable = res.Tried, res.Downloaded, res.Read.Done, res.Read.Counts["ok"]
	if len(res.Outcomes) > 0 {
		out.Failed = res.Outcomes
	}
	out.WithPDF, _ = t.lib.FetchedOnDisk(context.WithoutCancel(ctx))
	if err != nil {
		return nil, fetchOut{}, fmt.Errorf("%w (%d PDFs were downloaded and kept before it failed)", fetchError(err), res.Downloaded)
	}
	switch {
	case res.Tried == 0:
		out.Note = "Nothing to download: every paper with a legal PDF link has its PDF, or its links failed within the last month."
	case res.Tried == max:
		out.Note = "Stopped at max_pdfs. Call fetch_pdfs again to continue."
	}
	return nil, out, nil
}

func fetchError(err error) error {
	if errors.Is(err, library.ErrNoFolder) {
		return errors.New("this library is a single database file with no papers/ folder, so it cannot hold PDFs; " +
			"the user can give it one with fil library move")
	}
	return toolError(err)
}

// ---- library_stats: the PDFs ----

// maxWithoutTextShown caps the papers without text library_stats lists.
const maxWithoutTextShown = 20

type pdfState struct {
	WithPDF           int           `json:"papers_with_pdf"`
	Searchable        int           `json:"searchable" jsonschema:"papers whose PDF gave searchable text"`
	CitationSentences int           `json:"citations_with_sentence" jsonschema:"citations made by papers with text that carry the sentence they are in"`
	WithoutText       []NoTextPaper `json:"without_text,omitempty" jsonschema:"papers whose PDF gave no searchable text, and why"`
	WithoutTextOmit   int           `json:"without_text_omitted,omitempty"`
	MaybeOtherPaper   []Work        `json:"pdf_may_be_another_paper,omitempty" jsonschema:"papers whose downloaded PDF's first pages do not carry their title and author"`
}

// NoTextPaper is a paper whose PDF gave no searchable text.
type NoTextPaper struct {
	Paper  Work   `json:"paper"`
	Status string `json:"status" jsonschema:"no-text (a scan), gibberish, unreadable or timed-out"`
	Reason string `json:"reason,omitempty"`
}

// pdfState is nil for a library with no folder: it holds no PDFs.
func (t tools) pdfState(ctx context.Context) (*pdfState, error) {
	if t.lib.Folder() == "" {
		return nil, nil
	}
	var p pdfState
	var err error
	if p.WithPDF, err = t.lib.FetchedOnDisk(ctx); err != nil {
		return nil, err
	}
	counts, err := t.lib.TextCounts(ctx)
	if err != nil {
		return nil, err
	}
	p.Searchable = counts["ok"]
	c, err := t.lib.ContextCounts(ctx)
	if err != nil {
		return nil, err
	}
	p.CitationSentences = c.WithContext
	none, err := t.lib.WithoutText(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range none[:min(len(none), maxWithoutTextShown)] {
		p.WithoutText = append(p.WithoutText, NoTextPaper{Paper: toWork(n.Work), Status: n.Status, Reason: n.Reason})
	}
	p.WithoutTextOmit = len(none) - len(p.WithoutText)
	rows, err := t.lib.List(ctx, library.ListOptions{WithPDF: true})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.Mismatch {
			w, err := t.lib.Find(ctx, r.FilID)
			if err != nil {
				return nil, err
			}
			p.MaybeOtherPaper = append(p.MaybeOtherPaper, toWork(w))
		}
	}
	return &p, nil
}

// ---- get_paper ----

type paperIn struct {
	Identifier string `json:"identifier" jsonschema:"the paper: a fil ID, OpenAlex ID, DOI, arXiv ID, PMID or title, already in the library"`
}

// Sentence is a place the paper cites one of its references.
type Sentence struct {
	Cited    Work   `json:"cited"`
	Page     int    `json:"page"`
	Sentence string `json:"sentence"`
	Section  string `json:"section,omitempty"`
}

type paperOut struct {
	Paper     Work       `json:"paper"`
	Cites     int        `json:"cites" jsonschema:"works it cites"`
	CitedBy   int        `json:"cited_by_here" jsonschema:"works in this library that cite it"`
	PDF       string     `json:"pdf" jsonschema:"on_disk, or why there is no PDF"`
	PDFPath   string     `json:"pdf_path,omitempty"`
	Mismatch  bool       `json:"pdf_may_be_another_edition,omitempty" jsonschema:"the PDF's first pages do not carry this paper's title and author"`
	Text      string     `json:"text" jsonschema:"searchable, not_read, or why there is no text"`
	Pages     int        `json:"pages,omitempty"`
	Contexts  int        `json:"references_with_sentence" jsonschema:"how many of its references carry the sentence they are cited in"`
	Sentences []Sentence `json:"sentences,omitempty" jsonschema:"the first few"`
}

func (t tools) getPaper(ctx context.Context, _ *mcp.CallToolRequest, in paperIn) (*mcp.CallToolResult, paperOut, error) {
	p, err := t.lib.Show(ctx, in.Identifier)
	if err != nil {
		return nil, paperOut{}, toolError(err)
	}
	out := paperOut{Paper: toWork(p.Work), Cites: p.Cites, CitedBy: p.CitedBy, Contexts: p.Contexts}
	for _, f := range p.Files {
		if !f.Missing {
			out.PDF, out.PDFPath, out.Mismatch = "on_disk", f.Path, f.Mismatch
			break
		}
	}
	if out.PDF == "" {
		out.PDF = noPDFReason(p)
	}
	switch {
	case p.Text == nil:
		out.Text = "not_read"
	case p.Text.Status == "ok":
		out.Text, out.Pages = "searchable", p.Text.Pages
	default:
		out.Text = p.Text.Status
		if p.Text.Reason != "" {
			out.Text += ": " + p.Text.Reason
		}
	}
	for _, c := range p.Sample {
		out.Sentences = append(out.Sentences, Sentence{Cited: toWork(c.Work), Page: c.Page, Sentence: c.Sentence, Section: c.Section})
	}
	return nil, out, nil
}

func noPDFReason(p library.Paper) string {
	for _, f := range p.Files {
		if f.Missing {
			return "deleted_by_user"
		}
	}
	if len(p.Attempts) > 0 {
		return "every_link_failed"
	}
	switch p.PDF.Availability {
	case model.PDFLink:
		return "link_known_not_fetched"
	case model.PDFOpenNoLink:
		return "open_as_web_page_only"
	case model.PDFNoOpenCopy:
		return "no_open_copy"
	case model.PDFNotInOpenAlex:
		return "not_in_openalex"
	}
	return "not_looked_up"
}
