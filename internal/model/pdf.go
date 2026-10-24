package model

// PDFAvailability is what fil knows about a work's open-access PDF (ADR-013).
//
// Every work has an answer, and a work without a PDF always says why — the
// same way reference coverage does (D12). Without that, a library where one
// paper in eight has a PDF (spike 9) reads as a broken tool rather than as
// the state of open access.
type PDFAvailability string

const (
	// PDFUnchecked: not looked up yet. The zero value.
	PDFUnchecked PDFAvailability = ""
	// PDFLink: at least one open-access location offers a direct PDF link.
	PDFLink PDFAvailability = "pdf-link"
	// PDFOpenNoLink: open access, but only through landing pages.
	PDFOpenNoLink PDFAvailability = "open-no-pdf"
	// PDFNoOpenCopy: OpenAlex knows no open-access copy.
	PDFNoOpenCopy PDFAvailability = "no-open-copy"
	// PDFNotInOpenAlex: OpenAlex has no record of the work at all.
	PDFNotInOpenAlex PDFAvailability = "not-in-openalex"
)

// Location is one place a legal copy of a work's PDF can be downloaded from.
type Location struct {
	PDFURL     string
	LandingURL string // the page the PDF belongs to, if known
	Version    string // published | accepted | preprint, or empty when not stated
	License    string // e.g. cc-by; empty when none is stated, as for bronze
	Host       string // the PDF link's host name, without www.
}

// PDFLinks is a work's open-access PDF locations, in the order to try them.
type PDFLinks struct {
	Availability PDFAvailability
	Links        []Location
}
