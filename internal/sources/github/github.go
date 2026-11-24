// Package github asks GitHub which release of Filiation is the latest, and
// downloads that release's files. It is the one place the app talks to
// GitHub (D38), so the rules live here:
//
//   - a request names the app and its version in the User-Agent, and carries
//     nothing else: no email, no library, nothing about the person;
//   - asking for the latest release has a short timeout, so a slow network
//     never holds up the window;
//   - a file is streamed to the writer it is given, never held in memory, and
//     refused past a size no release of ours comes near.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBase is GitHub's API.
const DefaultBase = "https://api.github.com"

// Repo is where Filiation's releases are published.
const Repo = "codevector-2003/filiation"

const (
	askTimeout = 10 * time.Second
	maxFile    = 500 << 20 // the installer is about 60 MB
	maxText    = 1 << 20   // checksums.txt
)

// ErrOffline is a request that never reached GitHub: no network, or a name
// that did not resolve.
var ErrOffline = errors.New("github: could not reach GitHub")

// Release is one published release.
type Release struct {
	Tag       string    // v0.4.4
	Version   string    // 0.4.4
	Published time.Time // when it was published
	Notes     string    // its description, as written on GitHub
	Page      string    // its page on github.com
	Assets    []Asset   // the files attached to it
}

// Asset is one file attached to a release.
type Asset struct {
	Name string
	URL  string // where to download it
	Size int64
}

// Asset returns the file of that name, if the release has one.
func (r Release) Asset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// Client talks to GitHub.
type Client struct {
	Base      string       // the API; DefaultBase when empty
	Repo      string       // owner/name; Repo when empty
	UserAgent string       // "Filiation/0.4.3"
	HTTP      *http.Client // http.DefaultClient when nil
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) get(ctx context.Context, url, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	ua := c.UserAgent
	if ua == "" {
		ua = "Filiation"
	}
	req.Header.Set("User-Agent", ua)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	res, err := c.http().Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %v", ErrOffline, err)
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("github: %s answered %s", url, res.Status)
	}
	return res, nil
}

// Latest returns the latest published release: not a draft, not a
// pre-release, as GitHub decides.
func (c *Client) Latest(ctx context.Context) (Release, error) {
	ctx, cancel := context.WithTimeout(ctx, askTimeout)
	defer cancel()
	base, repo := c.Base, c.Repo
	if base == "" {
		base = DefaultBase
	}
	if repo == "" {
		repo = Repo
	}
	res, err := c.get(ctx, strings.TrimRight(base, "/")+"/repos/"+repo+"/releases/latest", "application/vnd.github+json")
	if err != nil {
		return Release{}, err
	}
	defer res.Body.Close()
	var raw struct {
		Tag       string    `json:"tag_name"`
		Body      string    `json:"body"`
		Page      string    `json:"html_url"`
		Published time.Time `json:"published_at"`
		Assets    []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, maxText)).Decode(&raw); err != nil {
		return Release{}, fmt.Errorf("github: read the latest release: %w", err)
	}
	if raw.Tag == "" {
		return Release{}, errors.New("github: the latest release has no tag")
	}
	r := Release{Tag: raw.Tag, Version: strings.TrimPrefix(raw.Tag, "v"), Published: raw.Published, Notes: raw.Body, Page: raw.Page}
	for _, a := range raw.Assets {
		r.Assets = append(r.Assets, Asset{Name: a.Name, URL: a.URL, Size: a.Size})
	}
	return r, nil
}

// Text downloads a small text file, such as checksums.txt.
func (c *Client) Text(ctx context.Context, a Asset) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, askTimeout)
	defer cancel()
	res, err := c.get(ctx, a.URL, "")
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, maxText+1))
	if err != nil {
		return "", fmt.Errorf("github: download %s: %w", a.Name, err)
	}
	if len(b) > maxText {
		return "", fmt.Errorf("github: %s is far larger than expected", a.Name)
	}
	return string(b), nil
}

// Download streams a file to w, telling progress how much has arrived of
// how much, as it goes. The release's own figure for the file's size is the
// one it must reach; the server's is used only when the release has none.
func (c *Client) Download(ctx context.Context, a Asset, w io.Writer, progress func(done, total int64)) error {
	res, err := c.get(ctx, a.URL, "application/octet-stream")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	total := a.Size
	if total <= 0 {
		total = res.ContentLength
	}
	if total > maxFile {
		return fmt.Errorf("github: %s is far larger than expected", a.Name)
	}
	var done int64
	buf := make([]byte, 64<<10)
	for {
		n, err := res.Body.Read(buf)
		if n > 0 {
			done += int64(n)
			if done > maxFile {
				return fmt.Errorf("github: %s is far larger than expected", a.Name)
			}
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
			if progress != nil {
				progress(done, total)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("%w: the download stopped: %v", ErrOffline, err)
		}
	}
	if total > 0 && done != total {
		return fmt.Errorf("github: %s stopped at %d of %d bytes", a.Name, done, total)
	}
	return nil
}
