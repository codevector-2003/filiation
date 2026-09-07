package model

// Ptr returns a pointer to v.
//
// The optional fields on Work are pointers, which makes constructing one by
// hand — in a test, in a fixture, at a call site with a literal — needlessly
// wordy without this. It exists so that the pointer discipline on Work stays
// cheap enough that nobody is tempted to argue it away.
func Ptr[T any](v T) *T { return &v }

// Deref returns *p, or fallback if p is nil.
func Deref[T any](p *T, fallback T) T {
	if p == nil {
		return fallback
	}
	return *p
}
