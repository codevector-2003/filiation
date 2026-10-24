package main

import (
	"fmt"
	"os"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/ledongthuc/pdf"
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
}

func newPdfium() (*pdfiumPool, error) {
	pool, err := webassembly.Init(webassembly.Config{MinIdle: 1, MaxIdle: 1, MaxTotal: 1})
	if err != nil {
		return nil, err
	}
	return &pdfiumPool{pool: pool}, nil
}

func (p *pdfiumPool) Close() { p.pool.Close() }

// extract reads the file into memory and hands PDFium the bytes, so the
// sandbox needs no view of the file system.
func (p *pdfiumPool) extract(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	inst, err := p.pool.GetInstance(30 * time.Second)
	if err != nil {
		return nil, err
	}
	defer inst.Close()
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
