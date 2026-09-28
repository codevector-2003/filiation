package identity

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/errs"
)

func TestNewFilIDShape(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool)
	for range 2000 {
		id := NewFilID()
		if len(id) != FilIDLength || id[0] != 'F' {
			t.Fatalf("NewFilID() = %q, want F and 8 characters", id)
		}
		if strings.ContainsAny(id[1:], "ILOU") {
			t.Fatalf("NewFilID() = %q uses a letter Crockford base32 leaves out", id)
		}
		if strings.ToUpper(id) != id {
			t.Fatalf("NewFilID() = %q, want upper case", id)
		}
		if !hasDigit(id[1:]) {
			t.Fatalf("NewFilID() = %q has no digit, so a word could be mistaken for it", id)
		}
		if !IsFilID(id) {
			t.Fatalf("IsFilID(NewFilID()) is false for %q", id)
		}
		if got, err := NormaliseFilID(id); err != nil || got != id {
			t.Fatalf("NormaliseFilID(%q) = %q, %v; want it unchanged", id, got, err)
		}
		seen[id] = true
	}
	// 2,000 draws from 40 bits: a repeat here means the randomness is broken,
	// not that we were unlucky.
	if len(seen) != 2000 {
		t.Errorf("2000 IDs contained %d repeats", 2000-len(seen))
	}
}

// Every byte value maps to the alphabet by its low five bits, so the bytes
// decide the ID exactly — which is what makes this test possible.
func TestNewFilIDFromBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		bytes []byte
		want  string
	}{
		{"low values are digits", []byte{0, 1, 2, 3, 4, 5, 6, 7}, "F01234567"},
		{"high bits ignored", []byte{32, 33, 34, 35, 36, 37, 38, 39}, "F01234567"},
		{"top of the alphabet", []byte{24, 25, 26, 27, 28, 29, 30, 9}, "FRSTVWXY9"},
		{"last letter", []byte{31, 31, 31, 31, 31, 31, 31, 0}, "FZZZZZZZ0"},
		// Eight letters and no digit is drawn again from the next eight bytes.
		{"no digit is redrawn",
			[]byte{10, 11, 12, 13, 14, 15, 16, 17, 10, 11, 12, 13, 14, 15, 16, 1},
			"FABCDEFG1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := newFilID(bytes.NewReader(tt.bytes))
			if err != nil {
				t.Fatalf("newFilID: %v", err)
			}
			if got != tt.want {
				t.Errorf("newFilID(%v) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

func TestNewFilIDShortRead(t *testing.T) {
	t.Parallel()
	if _, err := newFilID(bytes.NewReader([]byte{1, 2, 3})); err == nil {
		t.Error("newFilID with three bytes of randomness succeeded; want an error")
	}
}

func TestNormaliseFilID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string // empty means refused
	}{
		{"canonical", "F7K2M9QXA", "F7K2M9QXA"},
		{"lower case", "f7k2m9qxa", "F7K2M9QXA"},
		{"mixed case", "f7K2m9Qxa", "F7K2M9QXA"},
		{"surrounding space", "  F7K2M9QXA\n", "F7K2M9QXA"},
		{"all digits", "F12345678", "F12345678"},

		{"no digit", "FASTTRACK", ""},
		{"too short", "F7K2M9QX", ""},
		{"too long", "F7K2M9QXAB", ""},
		{"wrong prefix", "G7K2M9QXA", ""},
		{"excluded letter I", "F7K2M9QXI", ""},
		{"excluded letter L", "F7K2M9QXL", ""},
		{"excluded letter O", "F7K2M9QXO", ""},
		{"excluded letter U", "F7K2M9QXU", ""},
		{"no lookalike substitution", "FOK2M9QXA", ""},
		{"punctuation", "F7K2-9QXA", ""},
		{"openalex id", "W2741809807", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormaliseFilID(tt.input)
			if tt.want == "" {
				if !errors.Is(err, errs.ErrInvalidInput) {
					t.Errorf("NormaliseFilID(%q) = %q, %v; want ErrInvalidInput", tt.input, got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("NormaliseFilID(%q) = %q, %v; want %q", tt.input, got, err, tt.want)
			}
		})
	}
}

// A fil ID is recognised by Parse, and only a string that really is one: a
// title that happens to look like one must still be searched as a title.
func TestParseFilID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		kind  Kind
		value string
	}{
		{"F7K2M9QXA", KindFil, "F7K2M9QXA"},
		{"f7k2m9qxa", KindFil, "F7K2M9QXA"},
		{" F7K2M9QXA ", KindFil, "F7K2M9QXA"},
		// Nine letters, no digit: a word, so a title.
		{"Fasttrack", KindTitle, "Fasttrack"},
		// Crockford lookalikes are not substituted, so these stay titles.
		{"Framework", KindTitle, "Framework"},
		{"Frequency", KindTitle, "Frequency"},
	}
	for _, tt := range tests {
		got, err := Parse(tt.input)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tt.input, err)
		}
		if got.Kind != tt.kind || got.Value != tt.value {
			t.Errorf("Parse(%q) = %s %q, want %s %q", tt.input, got.Kind, got.Value, tt.kind, tt.value)
		}
	}
}
