package errs

import (
	"errors"
	"fmt"
	"testing"
)

// all is every sentinel the package exports. Tests that must hold for the whole
// set range over this, so adding a sentinel without adding a test is not
// possible by accident.
var all = map[string]error{
	"ErrNotFound":      ErrNotFound,
	"ErrTransient":     ErrTransient,
	"ErrUnresolved":    ErrUnresolved,
	"ErrAmbiguous":     ErrAmbiguous,
	"ErrInvalidConfig": ErrInvalidConfig,
	"ErrSchemaTooNew":  ErrSchemaTooNew,
}

// Sentinels are returned wrapped, so errors.Is must see through the wrapping.
// This is the one property every caller depends on.
func TestSentinelsSurviveWrapping(t *testing.T) {
	t.Parallel()
	for name, sentinel := range all {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			wrapped := fmt.Errorf("hydrate W2741809807: %w", sentinel)
			if !errors.Is(wrapped, sentinel) {
				t.Errorf("errors.Is(wrapped, %s) = false, want true", name)
			}
			doubly := fmt.Errorf("expand: %w", wrapped)
			if !errors.Is(doubly, sentinel) {
				t.Errorf("errors.Is(doubly wrapped, %s) = false, want true", name)
			}
		})
	}
}

// Each sentinel must be distinct from every other. Two declared as the same
// value, or as the same message, would make a caller branch on the wrong one —
// and the compiler would say nothing.
func TestSentinelsAreDistinct(t *testing.T) {
	t.Parallel()
	for name, sentinel := range all {
		for otherName, other := range all {
			if name == otherName {
				continue
			}
			if errors.Is(sentinel, other) {
				t.Errorf("errors.Is(%s, %s) = true, want false", name, otherName)
			}
		}
	}
}

// A sentinel is identified by value, not by text, but an empty or duplicated
// message would reach the user as a blank line.
func TestSentinelMessages(t *testing.T) {
	t.Parallel()
	seen := make(map[string]string, len(all))
	for name, sentinel := range all {
		msg := sentinel.Error()
		if msg == "" {
			t.Errorf("%s has an empty message", name)
		}
		if prev, dup := seen[msg]; dup {
			t.Errorf("%s and %s share the message %q", name, prev, msg)
		}
		seen[msg] = name
	}
}

// The wrapped form is what the user actually reads. Context first, sentinel
// last.
func TestWrappedMessageKeepsContext(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("hydrate %s: %w", "W2741809807", ErrUnresolved)
	const want = "hydrate W2741809807: unresolved identifier"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
