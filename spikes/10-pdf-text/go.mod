// Spike 10 is its own module so that the candidate PDF libraries never enter
// fil's go.mod until one is chosen (ADR-009).
module github.com/codevector-2003/filiation/spikes/10-pdf-text

go 1.26.0

require (
	github.com/klippa-app/go-pdfium v1.21.3
	github.com/ledongthuc/pdf v0.0.0-20261010181851-4e1f3a8729f5
	golang.org/x/text v0.42.0
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/jolestar/go-commons-pool/v2 v2.1.2 // indirect
	github.com/tetratelabs/wazero v1.12.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
)
