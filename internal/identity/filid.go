package identity

import (
	"crypto/rand"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/codevector-2003/filiation/internal/errs"
)

// A fil ID is Filiation's own name for a work: "F" and eight characters of
// Crockford base32, for example "F7K2M9QXA" (D17, ADR-010).
//
// It is the primary key of the work table. The OpenAlex ID stays the
// deduplication key for every work OpenAlex knows, but it cannot name a PDF the
// user owns that OpenAlex does not know, and it changes when OpenAlex merges two
// records. A fil ID is created once, on the user's machine, and never changes —
// which is why it carries no meaning: nothing about a paper can be corrected in
// a way that would force a new one.

// filIDAlphabet is Crockford's base32: digits and upper-case letters without
// I, L, O and U. The four are left out so an ID read aloud or copied by hand
// cannot be misread (I and L look like 1, O like 0), and so it cannot spell
// most words.
const filIDAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// FilIDLength is the length of a fil ID, prefix included.
const FilIDLength = 9

// filIDPattern is the shape of a fil ID, in either case. It does not check the
// digit rule; IsFilID does.
var filIDPattern = regexp.MustCompile(`^[Ff][0-9A-HJKMNP-TV-Za-hjkmnp-tv-z]{8}$`)

// NewFilID returns a fresh random fil ID.
//
// Uniqueness within a library is not this function's promise: 40 random bits
// make a clash rare, not impossible, and the store checks each new ID against
// the library inside the single writer before using it. This only guarantees
// the shape.
func NewFilID() string {
	id, err := newFilID(rand.Reader)
	if err != nil {
		// crypto/rand.Reader does not fail on any supported platform since Go
		// 1.24; if it ever did, no ID would be safe to hand out.
		panic(fmt.Sprintf("identity: no randomness for a fil ID: %v", err))
	}
	return id
}

// newFilID draws a fil ID from r. It is separate from NewFilID so that tests
// can supply the bytes.
//
// Each byte's low five bits pick a character. 256 is a multiple of 32, so
// every character is equally likely. An ID with no digit is drawn again: a
// digit is what keeps a nine-letter word beginning with F — "FASTTRACK" — from
// ever being read as an ID, and it costs a redraw about one time in twenty.
func newFilID(r io.Reader) (string, error) {
	var buf [FilIDLength - 1]byte
	for {
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return "", err
		}
		var b strings.Builder
		b.Grow(FilIDLength)
		b.WriteByte('F')
		for _, c := range buf {
			b.WriteByte(filIDAlphabet[c&31])
		}
		if id := b.String(); hasDigit(id[1:]) {
			return id, nil
		}
	}
}

// IsFilID reports whether s is a fil ID, in either case.
func IsFilID(s string) bool {
	return filIDPattern.MatchString(s) && hasDigit(s[1:])
}

// NormaliseFilID returns the canonical, upper-case form of a fil ID.
//
// Input is case-insensitive, because people type IDs; output is always upper
// case, because the store compares exactly. Lookalike substitution — O for 0,
// I or L for 1, which Crockford allows — is deliberately not done: every
// substitution makes more ordinary words parse as IDs.
func NormaliseFilID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !IsFilID(s) {
		return "", fmt.Errorf("identity: %q is not a fil ID such as F7K2M9QXA: %w",
			s, errs.ErrInvalidInput)
	}
	return strings.ToUpper(s), nil
}

func hasDigit(s string) bool {
	return strings.ContainsAny(s, "0123456789")
}
