// Package update keeps the desktop app up to date (D38). It asks GitHub for
// the latest release, says whether it is newer than the app, and on Windows,
// where the app was installed by Setup, downloads that release's installer,
// checks it against the release's checksums, and starts it to install the new
// version over this one and open it again.
//
// Nothing about the person or their library is sent: only a request for the
// latest release, naming the app and its version.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/codevector-2003/filiation/internal/sources/github"
)

// Phases of an update, as Status reports them.
const (
	Idle        = "idle"
	Downloading = "downloading"
	Checking    = "checking" // the download is checked against its checksum
	Ready       = "ready"    // checked; Install may run it
	Failed      = "failed"
)

// ErrBusy is a download asked for while one is under way.
var ErrBusy = errors.New("an update is already downloading")

// Info is what a check found.
type Info struct {
	Current    string    `json:"current"`
	Latest     string    `json:"latest"`
	Newer      bool      `json:"newer"`
	Published  time.Time `json:"published"`
	Notes      []string  `json:"notes"`      // a few lines of what is new
	Page       string    `json:"page"`       // the release on github.com
	CanInstall bool      `json:"canInstall"` // this app can install it itself
	Size       int64     `json:"size"`       // the installer's size, when it can
}

// State is how a download is going.
type State struct {
	Phase   string `json:"phase"`
	Version string `json:"version,omitempty"`
	Done    int64  `json:"done"`
	Total   int64  `json:"total"`
	Error   string `json:"error,omitempty"`
}

// Updater checks for, downloads and installs new versions. One serves the
// whole app, across libraries.
type Updater struct {
	Current    string         // the app's version
	Source     *github.Client // where releases come from
	Dir        string         // where installers are downloaded
	CanInstall bool           // the app was installed by Setup, on Windows
	Arch       string         // the installer's architecture: amd64
	// Start runs a program and does not wait for it: exec, or a stand-in in
	// a test, which must never run Setup.
	Start func(path string, args ...string) error

	mu      sync.Mutex
	state   State
	cancel  context.CancelFunc
	release *github.Release // the release last checked
	file    string          // the checked installer, once Ready
}

// installerName is the installer a release carries for this machine.
func (u *Updater) installerName(version string) string {
	arch := u.Arch
	if arch == "" {
		arch = "amd64"
	}
	return "Filiation-Setup-" + version + "-windows-" + arch + ".exe"
}

// Check asks for the latest release.
func (u *Updater) Check(ctx context.Context) (Info, error) {
	r, err := u.Source.Latest(ctx)
	if err != nil {
		return Info{Current: u.Current}, err
	}
	u.mu.Lock()
	u.release = &r
	u.mu.Unlock()
	info := Info{
		Current:   u.Current,
		Latest:    r.Version,
		Newer:     Newer(r.Version, u.Current),
		Published: r.Published,
		Notes:     notes(r.Notes),
		Page:      r.Page,
	}
	if a, ok := r.Asset(u.installerName(r.Version)); ok && u.CanInstall {
		if _, ok := r.Asset("checksums.txt"); ok {
			info.CanInstall = true
			info.Size = a.Size
		}
	}
	return info, nil
}

// Status says how a download is going.
func (u *Updater) Status() State {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.state.Phase == "" {
		return State{Phase: Idle}
	}
	return u.state
}

// Download fetches and checks the installer of the release last checked, in
// the background; Status follows it.
func (u *Updater) Download() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.state.Phase == Downloading || u.state.Phase == Checking {
		return ErrBusy
	}
	r := u.release
	if r == nil || !Newer(r.Version, u.Current) {
		return errors.New("there is no newer version to download")
	}
	if !u.CanInstall {
		return errors.New("this copy of Filiation cannot install updates itself")
	}
	setup, ok := r.Asset(u.installerName(r.Version))
	sums, ok2 := r.Asset("checksums.txt")
	if !ok || !ok2 {
		return fmt.Errorf("release %s has no installer for this computer", r.Version)
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.cancel = cancel
	u.file = ""
	u.state = State{Phase: Downloading, Version: r.Version, Total: setup.Size}
	go u.fetch(ctx, setup, sums)
	return nil
}

// Cancel stops a download; what arrived is deleted.
func (u *Updater) Cancel() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.cancel != nil {
		u.cancel()
	}
}

func (u *Updater) set(f func(*State)) {
	u.mu.Lock()
	f(&u.state)
	u.mu.Unlock()
}

func (u *Updater) fetch(ctx context.Context, setup, sums github.Asset) {
	path, err := u.fetchAndCheck(ctx, setup, sums)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.cancel = nil
	switch {
	case errors.Is(err, context.Canceled):
		u.state = State{Phase: Idle}
	case err != nil:
		u.state = State{Phase: Failed, Version: u.state.Version, Done: u.state.Done, Total: u.state.Total, Error: err.Error()}
	default:
		u.file = path
		u.state.Phase = Ready
	}
}

// fetchAndCheck downloads the installer beside the checksums and keeps it
// only if its SHA-256 is the one the release lists. Older downloads go.
func (u *Updater) fetchAndCheck(ctx context.Context, setup, sums github.Asset) (string, error) {
	if err := os.MkdirAll(u.Dir, 0o755); err != nil {
		return "", err
	}
	old, _ := filepath.Glob(filepath.Join(u.Dir, "Filiation-Setup-*"))
	for _, f := range old {
		os.Remove(f)
	}
	list, err := u.Source.Text(ctx, sums)
	if err != nil {
		return "", fmt.Errorf("could not get the release's checksums: %w", err)
	}
	want, ok := checksum(list, setup.Name)
	if !ok {
		return "", errors.New("the release's checksums do not list its installer")
	}
	path := filepath.Join(u.Dir, setup.Name)
	part := path + ".part"
	f, err := os.Create(part)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	err = u.Source.Download(ctx, setup, io.MultiWriter(f, h), func(done, total int64) {
		u.set(func(s *State) { s.Done, s.Total = done, total })
	})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(part)
		if errors.Is(err, context.Canceled) {
			return "", err
		}
		return "", fmt.Errorf("the download did not finish: %w", err)
	}
	u.set(func(s *State) { s.Phase = Checking })
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		os.Remove(part)
		return "", errors.New("the download did not match its checksum, so it was not run")
	}
	if err := os.Rename(part, path); err != nil {
		os.Remove(part)
		return "", err
	}
	return path, nil
}

// InstallArgs are what Setup is started with: no windows, no questions, the
// app closed if it is still open, and opened again when Setup is done.
var InstallArgs = []string{"/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/CLOSEAPPLICATIONS", "/relaunch=yes"}

// Install starts the checked installer. The app should close straight
// after: Setup replaces it and opens the new version.
func (u *Updater) Install() error {
	u.mu.Lock()
	file := u.file
	ready := u.state.Phase == Ready
	u.mu.Unlock()
	if !ready || file == "" {
		return errors.New("the update has not been downloaded and checked")
	}
	if _, err := os.Stat(file); err != nil {
		return fmt.Errorf("the downloaded update has gone: %w", err)
	}
	start := u.Start
	if start == nil {
		start = startDetached
	}
	return start(file, InstallArgs...)
}

func startDetached(path string, args ...string) error {
	cmd := exec.Command(path, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// checksum finds a file's SHA-256 in a checksums.txt: lines of
// "<hex>  <name>", as sha256sum and GoReleaser write them.
func checksum(list, name string) (string, bool) {
	s := bufio.NewScanner(strings.NewReader(list))
	for s.Scan() {
		f := strings.Fields(s.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 {
			return strings.ToLower(f[0]), true
		}
	}
	return "", false
}

var changelogLine = regexp.MustCompile(`^\*\s+[0-9a-f]{7,40}\s+(.+?)(\s+\(#\d+\))?\s*$`)

// notes turns a release's description into a few lines of what is new: the
// subjects of GoReleaser's changelog, without their commit hashes.
func notes(body string) []string {
	out := []string{}
	for _, line := range strings.Split(body, "\n") {
		if m := changelogLine.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			out = append(out, m[1])
			if len(out) == 4 {
				break
			}
		}
	}
	return out
}

// Newer reports whether version a is newer than b. A development build,
// or anything that is not a version, is never offered an update, and never
// counts as newer.
func Newer(a, b string) bool {
	va, ok1 := parse(a)
	vb, ok2 := parse(b)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < 3; i++ {
		if va.n[i] != vb.n[i] {
			return va.n[i] > vb.n[i]
		}
	}
	// 0.5.0-rc.1 comes before 0.5.0.
	switch {
	case va.pre == vb.pre:
		return false
	case va.pre == "":
		return true
	case vb.pre == "":
		return false
	}
	return va.pre > vb.pre
}

type version struct {
	n   [3]int
	pre string
}

func parse(s string) (version, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	core, pre, _ := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	var v version
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return version{}, false
		}
		v.n[i] = n
	}
	v.pre = pre
	return v, true
}
