package library

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/codevector-2003/filiation/internal/blobs"
	"github.com/codevector-2003/filiation/internal/model"
	"github.com/codevector-2003/filiation/internal/store"
)

// ErrNoFolder is returned by what needs a library folder, from a library that
// is only a database: one made by v0.1 or v0.2, or opened with --db. fil
// library move gives it a folder.
var ErrNoFolder = errors.New("this library has no folder yet")

// Folder is the library folder, or "" for a library that is only a database.
func (l *Library) Folder() string {
	if l.files == nil {
		return ""
	}
	return l.files.Root()
}

// Cloud names the sync service the library folder is in, or is empty. Its
// database is then kept outside the folder.
func (l *Library) Cloud() string { return l.cfg.Cloud }

// File is a file that belongs to a work.
type File struct {
	Path    string // absolute
	Version string // published | accepted | preprint, or empty
	Size    int64
	Missing bool // gone from disk at the last scan
}

// WorkFolder is a work's folder in papers/.
type WorkFolder struct {
	Work    model.Work
	Path    string // absolute
	Created bool   // made by this call
	Renamed string // the folder's previous name, when this call renamed it
	Files   []File
	// LongPath warns that the folder's path is close to Windows' limit.
	LongPath bool
}

// FolderFor returns the folder of the work input names, creating it in papers/
// if the work has none.
//
// It scans first, so a folder the researcher moved is found where it is now
// rather than made again. A folder that still has the name fil gave it is
// renamed when the name would now be different — a stub has since been
// fetched, say, and has a title. A folder a person named is never renamed.
func (l *Library) FolderFor(ctx context.Context, input string) (WorkFolder, error) {
	if l.files == nil {
		return WorkFolder{}, ErrNoFolder
	}
	w, err := l.Find(ctx, input)
	if err != nil {
		return WorkFolder{}, err
	}
	ws := []model.Work{w}
	if err := l.withAuthors(ctx, ws); err != nil {
		return WorkFolder{}, err
	}
	w = ws[0]
	if _, err := l.Scan(ctx); err != nil {
		return WorkFolder{}, err
	}
	return l.ensureFolder(ctx, w)
}

// ensureFolder is FolderFor without the lookup and the scan, for a caller
// that has done both: w must carry its authors, which name the folder.
func (l *Library) ensureFolder(ctx context.Context, w model.Work) (WorkFolder, error) {
	out := WorkFolder{Work: w}
	want := blobs.FolderName(w)
	folders, err := l.db.FoldersOf(ctx, w.FilID)
	if err != nil {
		return WorkFolder{}, err
	}
	var rel string
	switch {
	case len(folders) > 0 && !folders[0].Missing:
		f := folders[0]
		rel = f.Path
		if f.GivenName != "" && blobs.Base(f.Path) == f.GivenName && want != f.GivenName {
			to, err := l.files.RenameFolder(f.Path, want)
			if err != nil {
				return WorkFolder{}, err
			}
			err = l.db.Tx(ctx, func(tx *store.Tx) error {
				return tx.MoveFolder(ctx, f.Path, store.Folder{Path: to, FilID: w.FilID, GivenName: want})
			})
			if err != nil {
				// Put the folder back, so disk and library still agree.
				if _, rerr := l.files.RenameFolder(to, f.GivenName); rerr != nil {
					return WorkFolder{}, errors.Join(err, rerr)
				}
				return WorkFolder{}, err
			}
			rel, out.Renamed = to, f.GivenName
		}
	default:
		if rel, err = l.files.MakeWorkFolder(want); err != nil {
			return WorkFolder{}, err
		}
		err := l.db.Tx(ctx, func(tx *store.Tx) error {
			return tx.PutFolder(ctx, store.Folder{Path: rel, FilID: w.FilID, GivenName: want})
		})
		if err != nil {
			return WorkFolder{}, err
		}
		out.Created = true
	}

	out.Path = l.files.Abs(rel)
	out.LongPath = len(out.Path) > blobs.LongPath
	files, err := l.db.FilesOf(ctx, w.FilID)
	if err != nil {
		return WorkFolder{}, err
	}
	for _, f := range files {
		out.Files = append(out.Files, File{Path: l.files.Abs(f.Path), Version: f.Version, Size: f.Size, Missing: f.Missing})
	}
	return out, nil
}

// Collection is one collection and how many works it holds.
type Collection struct {
	Name       string
	Works      int
	FromFolder bool // mirrors a folder in papers/
}

// Collections lists the library's collections.
func (l *Library) Collections(ctx context.Context) ([]Collection, error) {
	cs, err := l.db.Collections(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Collection, len(cs))
	for i, c := range cs {
		out[i] = Collection{Name: c.Name, Works: c.Works, FromFolder: c.FromFolder}
	}
	return out, nil
}

// ScanReport is what a scan of papers/ found.
type ScanReport struct {
	Folders  int // work folders on disk, found by fil ID or by their files
	Files    int // files of those works on disk
	NewFiles int // files hashed and recorded for the first time

	Moved   []Moved  // folders found somewhere new, moved or renamed by a person
	Missing []string // folders gone since the last scan, as they were recorded

	MissingFiles int      // files gone, in all
	Unknown      []string // folders named for a fil ID this library does not have
	Loose        []string // PDFs in papers/ not matched to a work: for import (ADR-014)

	Collections int      // collections that mirror folders, after the scan
	LongPaths   []string // paths close to Windows' 260-character limit
}

// Moved is a folder the scan found at a new path.
type Moved struct {
	From, To string // relative to the library folder
	// ByHash means the folder's name no longer carries its fil ID, and it was
	// recognised by the hashes of the files in it.
	ByHash bool
}

// Scan brings the library's record of papers/ up to date with what is on disk
// (ADR-012). It runs when a command needs it, not continuously.
//
// A work folder is recognised by the fil ID at the end of its name, so a
// folder the researcher moved or renamed is found wherever it now sits. A
// folder whose ID was taken off its name is recognised by the hashes of its
// files. A folder that is gone is marked missing — the paper stays in the
// graph, because the citation is still true, and fil will not download it
// again unasked. New files in a work folder are hashed and recorded with it.
// Nothing on disk is changed.
//
// Hashing happens before the write transaction opens, so the single writer is
// held only for the update itself.
func (l *Library) Scan(ctx context.Context) (ScanReport, error) {
	if l.files == nil {
		return ScanReport{}, ErrNoFolder
	}
	listing, err := l.files.Walk()
	if err != nil {
		return ScanReport{}, err
	}
	dbFolders, err := l.db.Folders(ctx)
	if err != nil {
		return ScanReport{}, err
	}
	dbFiles, err := l.db.Files(ctx)
	if err != nil {
		return ScanReport{}, err
	}
	var ids []string
	for _, wf := range listing.WorkFolders {
		ids = append(ids, wf.FilID)
	}
	resolved, err := l.db.ResolveRefs(ctx, ids)
	if err != nil {
		return ScanReport{}, err
	}

	s := &scan{
		files:     l.files,
		folderAt:  map[string]store.Folder{},
		fileAt:    map[string]store.File{},
		present:   map[string]store.Folder{},
		fileSeen:  map[string]bool{},
		hashes:    map[string]string{},
		looseBy:   map[int64][]blobs.Entry{},
		claimed:   map[string]bool{},
		dirExists: map[string]bool{},
	}
	for _, f := range dbFolders {
		s.folderAt[f.Path] = f
	}
	for _, f := range dbFiles {
		s.fileAt[f.Path] = f
	}
	for _, d := range listing.Folders {
		s.dirExists[d] = true
	}
	for _, e := range listing.Loose {
		s.looseBy[e.Size] = append(s.looseBy[e.Size], e)
	}

	// Work folders on disk, by the fil ID in their names.
	var unmatched []blobs.WorkFolder
	for _, wf := range listing.WorkFolders {
		fil, ok := resolved[wf.FilID]
		if !ok {
			s.report.Unknown = append(s.report.Unknown, wf.Path)
			continue
		}
		row, known := s.folderAt[wf.Path]
		if known {
			s.keepFolder(store.Folder{Path: wf.Path, FilID: fil, GivenName: row.GivenName}, row)
		} else {
			unmatched = append(unmatched, wf)
		}
	}
	// Folders recorded at a path that is a plain folder now: found last time
	// by hash, and still there.
	for _, f := range dbFolders {
		if s.dirExists[f.Path] && !s.claimed[f.Path] {
			s.keepFolder(f, f)
		}
	}
	// Folders not where they were recorded: moved, unless the work's old
	// folder is still in its place too, which makes this a second folder.
	for _, wf := range unmatched {
		fil := resolved[wf.FilID]
		var from *store.Folder
		for _, f := range dbFolders {
			if f.FilID == fil && !s.onDisk(f.Path) && !s.claimed[f.Path] {
				from = &f
				break
			}
		}
		if from != nil {
			s.claimed[from.Path] = true
			s.moveFolder(from.Path, store.Folder{Path: wf.Path, FilID: fil, GivenName: from.GivenName}, false)
		} else {
			s.putFolder(store.Folder{Path: wf.Path, FilID: fil})
		}
	}

	// Files in the work folders on disk.
	for _, wf := range listing.WorkFolders {
		fil, ok := resolved[wf.FilID]
		if !ok {
			continue
		}
		for _, e := range wf.Files {
			if err := s.seeFile(e, fil); err != nil {
				return ScanReport{}, err
			}
		}
	}
	// Loose PDFs the library already knows by their path.
	for _, e := range listing.Loose {
		if row, ok := s.fileAt[e.Path]; ok {
			s.claimed[e.Path] = true
			if err := s.seeFile(e, row.FilID); err != nil {
				return ScanReport{}, err
			}
		}
	}

	// Recorded folders not on disk: look for their files by hash, else
	// mark them missing.
	for _, f := range dbFolders {
		if s.claimed[f.Path] || s.onDisk(f.Path) {
			continue
		}
		if err := s.recoverFolder(f); err != nil {
			return ScanReport{}, err
		}
	}
	// Recorded files still unseen, outside any recovered folder.
	for _, f := range s.sortedFiles() {
		if s.fileSeen[f.Path] {
			continue
		}
		e, ok, err := s.findByHash(f)
		if err != nil {
			return ScanReport{}, err
		}
		if ok {
			moved := f
			moved.Path, moved.Missing = e.Path, false
			s.fileOps = append(s.fileOps, fileOp{from: f.Path, to: moved})
			s.fileSeen[e.Path] = true
			s.report.Files++
			continue
		}
		if !f.Missing {
			f.Missing = true
			s.fileOps = append(s.fileOps, fileOp{from: f.Path, to: f})
		}
		s.report.MissingFiles++
	}

	for _, e := range listing.Loose {
		if e.IsPDF() && !s.claimed[e.Path] {
			s.report.Loose = append(s.report.Loose, l.files.Abs(e.Path))
		}
	}

	collections := map[string]store.FolderCollection{}
	if l.cfg.FolderCollections {
		collections = s.collections()
	}
	s.report.Collections = len(collections)
	s.report.Folders = len(s.present)
	for p := range s.present {
		if abs := l.files.Abs(p); len(abs) > blobs.LongPath {
			s.report.LongPaths = append(s.report.LongPaths, abs)
		}
	}
	sort.Strings(s.report.LongPaths)

	err = l.db.Tx(ctx, func(tx *store.Tx) error {
		for _, op := range s.folderOps {
			if err := tx.MoveFolder(ctx, op.from, op.to); err != nil {
				return err
			}
		}
		for _, op := range s.fileOps {
			if err := tx.MoveFile(ctx, op.from, op.to); err != nil {
				return err
			}
		}
		return tx.SyncFolderCollections(ctx, collections)
	})
	if err != nil {
		return ScanReport{}, err
	}
	return s.report, nil
}

// scan is one Scan in progress: what the library recorded, what is on disk,
// and the updates that reconcile them, applied together at the end.
type scan struct {
	files *blobs.Folder

	folderAt  map[string]store.Folder // recorded folders by path
	fileAt    map[string]store.File   // recorded files by path, as moves rewrite them
	present   map[string]store.Folder // folders on disk after the scan, by path
	fileSeen  map[string]bool
	hashes    map[string]string // path -> hash, for loose files hashed once
	looseBy   map[int64][]blobs.Entry
	claimed   map[string]bool // recorded folders and loose files accounted for
	dirExists map[string]bool // the researcher's folders on disk

	folderOps []folderOp
	fileOps   []fileOp
	report    ScanReport
}

type folderOp struct {
	from string
	to   store.Folder
}

type fileOp struct {
	from string
	to   store.File
}

func (s *scan) onDisk(path string) bool {
	_, ok := s.present[path]
	return ok
}

// keepFolder records a folder found where it was recorded, updating the row
// only if something about it changed.
func (s *scan) keepFolder(now, was store.Folder) {
	s.claimed[now.Path] = true
	s.present[now.Path] = now
	if was.Missing || was.FilID != now.FilID {
		s.folderOps = append(s.folderOps, folderOp{from: now.Path, to: now})
	}
}

func (s *scan) putFolder(f store.Folder) {
	s.present[f.Path] = f
	s.folderOps = append(s.folderOps, folderOp{from: f.Path, to: f})
}

// moveFolder records a folder found at a new path, and rewrites the recorded
// paths of its files to match, as the store will.
func (s *scan) moveFolder(from string, to store.Folder, byHash bool) {
	s.present[to.Path] = to
	s.folderOps = append(s.folderOps, folderOp{from: from, to: to})
	s.report.Moved = append(s.report.Moved, Moved{From: from, To: to.Path, ByHash: byHash})
	prefix := from + "/"
	for p, f := range s.fileAt {
		if strings.HasPrefix(p, prefix) {
			delete(s.fileAt, p)
			f.Path = to.Path + "/" + strings.TrimPrefix(p, prefix)
			s.fileAt[f.Path] = f
		}
	}
}

// seeFile reconciles one file on disk with what is recorded at its path,
// hashing it only when it is new or its size or time has changed.
func (s *scan) seeFile(e blobs.Entry, fil string) error {
	s.fileSeen[e.Path] = true
	s.report.Files++
	row, known := s.fileAt[e.Path]
	mtime := e.ModTime.Unix()
	if known && row.Size == e.Size && row.ModTime == mtime {
		if row.Missing || row.FilID != fil {
			row.Missing, row.FilID = false, fil
			s.fileOps = append(s.fileOps, fileOp{from: e.Path, to: row})
		}
		return nil
	}
	h, err := s.files.Hash(e.Path)
	if err != nil {
		return err
	}
	f := store.File{Path: e.Path, FilID: fil, SHA256: h.SHA256, Size: h.Size, ModTime: mtime,
		Version: blobs.FileVersion(blobs.Base(e.Path)), Origin: store.OriginFound}
	if known {
		f.Version, f.Origin = row.Version, row.Origin
	} else {
		s.report.NewFiles++
	}
	s.fileOps = append(s.fileOps, fileOp{from: e.Path, to: f})
	return nil
}

// findByHash looks among the loose PDFs of the same size for one holding f's
// bytes. Only same-size files are hashed, and each at most once.
func (s *scan) findByHash(f store.File) (blobs.Entry, bool, error) {
	for _, e := range s.looseBy[f.Size] {
		if s.claimed[e.Path] {
			continue
		}
		h, ok := s.hashes[e.Path]
		if !ok {
			got, err := s.files.Hash(e.Path)
			if err != nil {
				return blobs.Entry{}, false, err
			}
			h = got.SHA256
			s.hashes[e.Path] = h
		}
		if strings.EqualFold(h, f.SHA256) {
			s.claimed[e.Path] = true
			return e, true, nil
		}
	}
	return blobs.Entry{}, false, nil
}

// recoverFolder looks for a vanished folder's files by hash. If every one
// found sits in the same folder, that folder is this one, renamed without its
// fil ID. Otherwise the files are followed one by one and the folder is
// missing.
func (s *scan) recoverFolder(f store.Folder) error {
	prefix := f.Path + "/"
	var mine []store.File
	for p, file := range s.fileAt {
		if strings.HasPrefix(p, prefix) && !s.fileSeen[p] {
			mine = append(mine, file)
		}
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].Path < mine[j].Path })

	type found struct {
		file store.File
		at   blobs.Entry
	}
	var hits []found
	for _, file := range mine {
		e, ok, err := s.findByHash(file)
		if err != nil {
			return err
		}
		if ok {
			hits = append(hits, found{file, e})
		}
	}
	dir := ""
	for i, h := range hits {
		d := blobs.Dir(h.at.Path)
		if i == 0 {
			dir = d
		} else if d != dir {
			dir = ""
			break
		}
	}
	if dir != "" && dir != blobs.PapersDir && !s.onDisk(dir) {
		s.claimed[f.Path] = true
		s.moveFolder(f.Path, store.Folder{Path: dir, FilID: f.FilID, GivenName: f.GivenName}, true)
		for _, h := range hits {
			rewritten := dir + "/" + strings.TrimPrefix(h.file.Path, prefix)
			moved := h.file
			moved.Path, moved.Missing = h.at.Path, false
			s.fileOps = append(s.fileOps, fileOp{from: rewritten, to: moved})
			delete(s.fileAt, rewritten)
			s.fileSeen[h.at.Path] = true
			s.report.Files++
		}
		return nil
	}

	// Not recovered as a folder. Files found elsewhere are followed by the
	// pass over unseen files, which will find them claimed and look again —
	// so release them here.
	for _, h := range hits {
		delete(s.claimed, h.at.Path)
	}
	s.claimed[f.Path] = true
	if !f.Missing {
		s.report.Missing = append(s.report.Missing, f.Path)
		f.Missing = true
		s.folderOps = append(s.folderOps, folderOp{from: f.Path, to: f})
	}
	return nil
}

func (s *scan) sortedFiles() []store.File {
	out := make([]store.File, 0, len(s.fileAt))
	for _, f := range s.fileAt {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// collections makes each folder in papers/ that holds work folders, or files
// of works, a collection of those works (ADR-012). papers/Chapter 2/Key
// studies is the collection "Chapter 2/Key studies". Only what sits directly
// in a folder belongs to it.
func (s *scan) collections() map[string]store.FolderCollection {
	members := map[string]map[string]bool{}
	add := func(dir, fil string) {
		if dir == blobs.PapersDir || !strings.HasPrefix(dir, blobs.PapersDir+"/") {
			return
		}
		if members[dir] == nil {
			members[dir] = map[string]bool{}
		}
		members[dir][fil] = true
	}
	for p, f := range s.present {
		add(blobs.Dir(p), f.FilID)
	}
	for p, f := range s.fileAt {
		if s.fileSeen[p] && !s.underPresentFolder(p) {
			add(blobs.Dir(p), f.FilID)
		}
	}
	out := make(map[string]store.FolderCollection, len(members))
	for dir, fils := range members {
		ids := make([]string, 0, len(fils))
		for id := range fils {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		out[dir] = store.FolderCollection{Name: strings.TrimPrefix(dir, blobs.PapersDir+"/"), Works: ids}
	}
	return out
}

func (s *scan) underPresentFolder(p string) bool {
	for d := blobs.Dir(p); d != "." && d != blobs.PapersDir; d = blobs.Dir(d) {
		if s.onDisk(d) {
			return true
		}
	}
	return false
}
