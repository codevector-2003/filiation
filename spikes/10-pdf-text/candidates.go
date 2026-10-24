package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/ledongthuc/pdf"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

// ledongthuc extracts each page with github.com/ledongthuc/pdf.
func ledongthuc(path string) ([]string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	pages := make([]string, 0, r.NumPage())
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			pages = append(pages, "")
			continue
		}
		text, err := p.GetPlainText(nil)
		if err != nil {
			return pages, fmt.Errorf("page %d: %w", i, err)
		}
		pages = append(pages, text)
	}
	return pages, nil
}

// pdfiumPool is PDFium compiled to WebAssembly and run by wazero: no cgo.
type pdfiumPool struct {
	pool pdfium.Pool

	mu      sync.Mutex
	running pdfium.Pdfium // the instance extracting now, for kill
}

func newPdfium() (*pdfiumPool, error) {
	pool, err := webassembly.Init(webassembly.Config{
		// One extraction at a time, plus a spare: a killed instance can take
		// a moment to let go, and the next file must not wait on it.
		MinIdle: 1, MaxIdle: 1, MaxTotal: 2,
		// go-pdfium's own default, plus WithCloseOnContextDone: closing a
		// module then interrupts the code running in it, so kill stops an
		// extraction rather than abandoning it to run on. The bundled module
		// needs WebAssembly exception handling, which the default turns on.
		RuntimeConfig: wazero.NewRuntimeConfig().
			WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExceptionHandling).
			WithCloseOnContextDone(true),
	})
	if err != nil {
		return nil, err
	}
	return &pdfiumPool{pool: pool}, nil
}

func (p *pdfiumPool) Close() { p.pool.Close() }

// kill ends the extraction that has run past the time limit.
func (p *pdfiumPool) kill() {
	p.mu.Lock()
	inst := p.running
	p.running = nil
	p.mu.Unlock()
	if inst != nil {
		inst.Kill()
	}
}

// extract reads the file into memory and hands PDFium the bytes, so the
// sandbox needs no view of the file system.
func (p *pdfiumPool) extract(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	inst, err := p.pool.GetInstanceWithContext(ctx)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.running = inst
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		if p.running == inst {
			p.running = nil
		}
		p.mu.Unlock()
		inst.Close()
	}()

	doc, err := inst.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		return nil, err
	}
	defer inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc.Document})
	count, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc.Document})
	if err != nil {
		return nil, err
	}
	pages := make([]string, 0, count.PageCount)
	for i := range count.PageCount {
		t, err := inst.GetPageText(&requests.GetPageText{Page: requests.Page{
			ByIndex: &requests.PageByIndex{Document: doc.Document, Index: i},
		}})
		if err != nil {
			return pages, fmt.Errorf("page %d: %w", i+1, err)
		}
		pages = append(pages, t.Text)
	}
	return pages, nil
}
