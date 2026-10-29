package text

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/klippa-app/go-pdfium"
	pdfiumerrors "github.com/klippa-app/go-pdfium/errors"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

// ExtractorVersion names the extractor and the cleaning applied to its
// output. Text read by an older version is read again; raise it whenever a
// change here or in Clean would change what is stored.
const ExtractorVersion = "pdfium/3" // 2: citation context is found as a PDF is read; 3: spaced accents joined

// Extractor returns the text of each page of a PDF, in page order. It is an
// interface so PDFium can be replaced without touching cleaning or chunking
// (ADR-009).
type Extractor interface {
	Extract(ctx context.Context, pdf []byte) ([]string, error)
}

// ErrUnreadable is a PDF the extractor refused: damaged, encrypted, or not
// a PDF at all. One such file fails one paper, never the run.
var ErrUnreadable = errors.New("unreadable PDF")

// PDFium is PDFium compiled to WebAssembly and run by wazero: no cgo (D20).
// It starts on first use — compiling the module takes a moment — so a
// command that reads no PDF pays nothing. Safe for concurrent use; at most
// two extractions run at once.
type PDFium struct {
	mu       sync.Mutex
	cacheDir string

	once sync.Once
	pool pdfium.Pool
	err  error
}

// pdfiumInstances is how many extractions run at once.
const pdfiumInstances = 2

var shared PDFium

// Shared returns the process's one PDFium. Its module is compiled once and
// kept until the process ends, however many libraries are opened.
func Shared() *PDFium { return &shared }

// UseCache keeps the compiled module in dir, so the next process starts
// PDFium in a tenth of a second rather than three (measured: 3.1 s, then
// 80 ms; about 21 MB on disk). It takes effect only before the first
// extraction.
func (p *PDFium) UseCache(dir string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cacheDir == "" {
		p.cacheDir = dir
	}
}

func (p *PDFium) start() error {
	p.once.Do(func() {
		p.mu.Lock()
		dir := p.cacheDir
		p.mu.Unlock()
		if dir != "" {
			if c, err := wazero.NewCompilationCacheWithDir(dir); err == nil {
				if p.pool, p.err = initPool(c); p.err == nil {
					return
				}
			}
			// A cache that cannot be used — unwritable, or damaged — only
			// costs time.
		}
		if p.pool, p.err = initPool(nil); p.err != nil {
			p.err = fmt.Errorf("text: start PDFium: %w", p.err)
		}
	})
	return p.err
}

func initPool(cache wazero.CompilationCache) (pdfium.Pool, error) {
	// go-pdfium's own default runtime, plus WithCloseOnContextDone: closing a
	// module then interrupts the code running in it, so a time limit stops an
	// extraction rather than abandoning it to run on (spike 10). The bundled
	// module needs WebAssembly exception handling.
	rc := wazero.NewRuntimeConfig().
		WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExceptionHandling).
		WithCloseOnContextDone(true)
	if cache != nil {
		rc = rc.WithCompilationCache(cache)
	}
	return webassembly.Init(webassembly.Config{
		MinIdle: 0, MaxIdle: pdfiumInstances, MaxTotal: pdfiumInstances,
		// No mounts: PDFium is handed bytes, so its sandbox needs no view of
		// the file system. go-pdfium mounts the whole drive when this is nil.
		FSConfig:      wazero.NewFSConfig(),
		RuntimeConfig: rc,
		Stdout:        io.Discard,
		Stderr:        io.Discard,
	})
}

// Extract returns the text of each page. When ctx ends — a time limit, or
// the user cancelling — the extraction is stopped, not just abandoned, and
// ctx's error is returned. A PDF PDFium cannot open is ErrUnreadable.
func (p *PDFium) Extract(ctx context.Context, pdf []byte) ([]string, error) {
	var pages []string
	err := p.run(ctx, func(inst pdfium.Pdfium) (err error) {
		pages, err = extractPages(inst, pdf)
		return err
	})
	if err != nil {
		return nil, err
	}
	return pages, nil
}

// run calls fn with an instance of its own, and ends the instance after.
func (p *PDFium) run(ctx context.Context, fn func(pdfium.Pdfium) error) (err error) {
	if err := p.start(); err != nil {
		return err
	}
	inst, err := p.pool.GetInstanceWithContext(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("text: PDFium instance: %w", err)
	}

	// Exactly one of the two ends the instance: killed when ctx ends first,
	// closed when the work does. Killed means already gone, and closing it
	// as well would race Kill's own teardown.
	var end sync.Once
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			end.Do(func() { inst.Kill() })
		case <-done:
		}
	}()
	defer func() {
		close(done)
		end.Do(func() { inst.Close() })
		// A killed instance can fail or panic in the call it was in. Either
		// way, what happened is that ctx ended.
		if r := recover(); r != nil {
			err = fmt.Errorf("text: PDFium: %v", r)
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
	}()
	return fn(inst)
}

func extractPages(inst pdfium.Pdfium, pdf []byte) ([]string, error) {
	doc, err := inst.OpenDocument(&requests.OpenDocument{File: &pdf})
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrUnreadable, reason(err))
	}
	defer inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document}) //nolint:errcheck // nothing to do about it

	count, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrUnreadable, reason(err))
	}
	pages := make([]string, 0, count.PageCount)
	for i := range count.PageCount {
		t, err := inst.GetPageText(&requests.GetPageText{Page: requests.Page{
			ByIndex: &requests.PageByIndex{Document: doc.Document, Index: i},
		}})
		if err != nil {
			return nil, fmt.Errorf("%w: page %d: %s", ErrUnreadable, i+1, reason(err))
		}
		pages = append(pages, t.Text)
	}
	return pages, nil
}

// reason turns PDFium's errors into words a researcher can act on.
func reason(err error) string {
	switch {
	case errors.Is(err, pdfiumerrors.ErrPassword):
		return "password-protected"
	case errors.Is(err, pdfiumerrors.ErrSecurity):
		return "encrypted"
	case errors.Is(err, pdfiumerrors.ErrFormat), errors.Is(err, pdfiumerrors.ErrFile):
		return "damaged or not a PDF"
	}
	return err.Error()
}
