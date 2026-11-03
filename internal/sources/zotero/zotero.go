package zotero

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// DefaultBase is where Zotero serves its local API.
const DefaultBase = "http://127.0.0.1:23119/api"

// Why Zotero could not be read.
var (
	// ErrNotRunning: nothing answers on Zotero's port.
	ErrNotRunning = errors.New("zotero is not running on this computer")
	// ErrNotAllowed: Zotero answers, but has not been told to let other
	// applications read it (Settings, Advanced).
	ErrNotAllowed = errors.New("zotero is not letting other applications read it")
)

// Client reads one Zotero on this computer.
type Client struct {
	base string
	http *http.Client
}

// New returns a client for the local API at base; "" is DefaultBase.
func New(base string, hc *http.Client) *Client {
	if base == "" {
		base = DefaultBase
	}
	if hc == nil {
		hc = &http.Client{Timeout: 2 * time.Minute}
	}
	return &Client{base: strings.TrimSuffix(base, "/"), http: hc}
}

// get fetches one path under the API and returns its body.
func (c *Client) get(ctx context.Context, path string) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Zotero-API-Version", "3")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, fmt.Errorf("%w: %v", ErrNotRunning, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("zotero: read %s: %w", path, err)
	}
	switch {
	case resp.StatusCode == http.StatusForbidden:
		return nil, nil, ErrNotAllowed
	case resp.StatusCode != http.StatusOK:
		return nil, nil, fmt.Errorf("zotero: %s answered %s", path, resp.Status)
	}
	return body, resp.Header, nil
}

// Check says whether Zotero is there and lets other applications read it.
func (c *Client) Check(ctx context.Context) error {
	_, _, err := c.get(ctx, "/users/0/collections?limit=1")
	return err
}

// Collection is one Zotero collection.
type Collection struct {
	Key    string
	Name   string
	Parent string // "" at the top
}

// Collections reads every collection in the user's own library.
func (c *Client) Collections(ctx context.Context) ([]Collection, error) {
	body, _, err := c.get(ctx, "/users/0/collections?format=json")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Data struct {
			Key              string          `json:"key"`
			Name             string          `json:"name"`
			ParentCollection json.RawMessage `json:"parentCollection"` // false, or a key
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("zotero: read collections: %w", err)
	}
	out := make([]Collection, 0, len(raw))
	for _, r := range raw {
		col := Collection{Key: r.Data.Key, Name: r.Data.Name}
		_ = json.Unmarshal(r.Data.ParentCollection, &col.Parent) // false leaves it ""
		out = append(out, col)
	}
	return out, nil
}

// Item is one Zotero item: a paper, book or other record, with its PDFs.
type Item struct {
	Key         string
	Version     int
	Type        string // Zotero's item type: journalArticle, book, preprint...
	Title       string
	Year        int // 0 when not known
	Creators    []string
	DOI         string
	ArXiv       string
	PMID        string
	Collections []string // keys
	Tags        []string // the ones the user added; automatic tags are left out
	PDFs        []Attachment
}

// Attachment is a PDF attached to an item.
type Attachment struct {
	Key      string
	LinkMode string // imported_file, imported_url, linked_file
	Title    string
}

type rawItem struct {
	Key  string `json:"key"`
	Data struct {
		Key         string   `json:"key"`
		Version     int      `json:"version"`
		ItemType    string   `json:"itemType"`
		Title       string   `json:"title"`
		Date        string   `json:"date"`
		DOI         string   `json:"DOI"`
		URL         string   `json:"url"`
		Extra       string   `json:"extra"`
		ArchiveID   string   `json:"archiveID"`
		ParentItem  string   `json:"parentItem"`
		LinkMode    string   `json:"linkMode"`
		ContentType string   `json:"contentType"`
		Collections []string `json:"collections"`
		Deleted     any      `json:"deleted"`
		Creators    []struct {
			FirstName string `json:"firstName"`
			LastName  string `json:"lastName"`
			Name      string `json:"name"`
		} `json:"creators"`
		Tags []struct {
			Tag  string `json:"tag"`
			Type int    `json:"type"`
		} `json:"tags"`
	} `json:"data"`
}

// Items reads every item in the user's own library, with its PDFs. Notes,
// annotations and items in the trash are left out.
func (c *Client) Items(ctx context.Context) ([]Item, error) {
	body, _, err := c.get(ctx, "/users/0/items?format=json")
	if err != nil {
		return nil, err
	}
	var raw []rawItem
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("zotero: read items: %w", err)
	}
	return items(raw), nil
}

func items(raw []rawItem) []Item {
	var out []Item
	at := map[string]int{}
	var attachments []rawItem
	for _, r := range raw {
		d := r.Data
		if trashed(d.Deleted) {
			continue
		}
		switch d.ItemType {
		case "note", "annotation":
			continue
		case "attachment":
			attachments = append(attachments, r)
			continue
		}
		it := Item{Key: d.Key, Version: d.Version, Type: d.ItemType, Title: strings.TrimSpace(d.Title),
			Year: year(d.Date), Collections: d.Collections}
		for _, cr := range d.Creators {
			if name := strings.TrimSpace(strings.TrimSpace(cr.FirstName + " " + cr.LastName)); name != "" {
				it.Creators = append(it.Creators, name)
			} else if cr.Name != "" {
				it.Creators = append(it.Creators, cr.Name)
			}
		}
		for _, t := range d.Tags {
			if t.Type == 0 && strings.TrimSpace(t.Tag) != "" {
				it.Tags = append(it.Tags, strings.TrimSpace(t.Tag))
			}
		}
		it.DOI, it.ArXiv, it.PMID = identifiers(d.DOI, d.URL, d.Extra, d.ArchiveID)
		at[it.Key] = len(out)
		out = append(out, it)
	}
	for _, r := range attachments {
		d := r.Data
		i, ok := at[d.ParentItem]
		if !ok || d.ContentType != "application/pdf" || d.LinkMode == "linked_url" {
			continue
		}
		out[i].PDFs = append(out[i].PDFs, Attachment{Key: d.Key, LinkMode: d.LinkMode, Title: d.Title})
	}
	return out
}

// trashed reads Zotero's deleted flag, which comes as 1, true or "1".
func trashed(v any) bool {
	switch d := v.(type) {
	case bool:
		return d
	case float64:
		return d != 0
	case string:
		return d != "" && d != "0"
	}
	return false
}

var yearIn = regexp.MustCompile(`\b(1[5-9]\d\d|20\d\d)\b`)

func year(date string) int {
	m := yearIn.FindString(date)
	if m == "" {
		return 0
	}
	var y int
	fmt.Sscan(m, &y)
	return y
}

var (
	doiIn   = regexp.MustCompile(`(?i)\b(10\.\d{4,9}/\S+)`)
	arxivIn = regexp.MustCompile(`(?i)arxiv\.org/(?:abs|pdf)/([^\s?#]+?)(?:v\d+)?(?:\.pdf)?(?:[?#]|$)`)
	extraIn = regexp.MustCompile(`(?im)^\s*(DOI|PMID|arXiv)\s*:\s*(\S+)\s*$`)
)

// identifiers finds a DOI, an arXiv ID and a PubMed ID among an item's fields:
// its DOI field, its archive ID, its URL, and the "Key: value" lines Zotero
// keeps in Extra for item types with no field of their own.
func identifiers(doiField, link, extra, archiveID string) (doi, arxiv, pmid string) {
	if m := doiIn.FindString(doiField); m != "" {
		doi = m
	}
	for _, m := range extraIn.FindAllStringSubmatch(extra, -1) {
		switch strings.ToLower(m[1]) {
		case "doi":
			if doi == "" {
				doi = doiIn.FindString(m[2])
			}
		case "pmid":
			pmid = m[2]
		case "arxiv":
			arxiv = m[2]
		}
	}
	if arxiv == "" {
		if v, ok := strings.CutPrefix(strings.ToLower(archiveID), "arxiv:"); ok {
			arxiv = archiveID[len(archiveID)-len(v):]
		} else if m := arxivIn.FindStringSubmatch(link); m != nil {
			arxiv = m[1]
		}
	}
	if doi == "" {
		if u, err := url.Parse(link); err == nil && strings.HasSuffix(strings.ToLower(u.Host), "doi.org") {
			doi = doiIn.FindString(strings.TrimPrefix(u.Path, "/"))
		}
	}
	return strings.TrimRight(doi, ".,;"), arxiv, pmid
}

// FilePath is where an attachment's file is on this computer. Zotero answers
// with a file URL, and the file may since have gone.
func (c *Client) FilePath(ctx context.Context, attachment string) (string, error) {
	body, _, err := c.get(ctx, "/users/0/items/"+url.PathEscape(attachment)+"/file/view/url")
	if err != nil {
		return "", err
	}
	return fromFileURL(strings.TrimSpace(string(body)))
}

func fromFileURL(s string) (string, error) {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "file" {
		return "", fmt.Errorf("zotero: %q is not a file on this computer", s)
	}
	p := u.Path
	if runtime.GOOS == "windows" {
		p = strings.TrimPrefix(p, "/")
		if u.Host != "" { // a network share: file://server/share/x.pdf
			p = `\\` + u.Host + "/" + p
		}
	}
	return filepath.FromSlash(p), nil
}
