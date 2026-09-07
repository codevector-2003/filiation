package model

import (
	"math"
	"testing"
)

func TestWorkIsStub(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		work Work
		want bool
	}{
		{"fresh from an edge", Work{OpenAlexID: "W1"}, true},
		{"hydrated", Work{OpenAlexID: "W1", Hydrated: true, Title: Ptr("A title")}, false},
		{"unresolved is still a stub", Work{OpenAlexID: "W1", Unresolved: true}, true},
		{"hydrated with no title is not a stub", Work{OpenAlexID: "W1", Hydrated: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.work.IsStub(); got != tt.want {
				t.Errorf("IsStub() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWorkDisplayTitle(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		work Work
		want string
	}{
		{
			name: "hydrated",
			work: Work{OpenAlexID: "W1", Hydrated: true, Title: Ptr("Filiation")},
			want: "Filiation",
		},
		{
			name: "stub",
			work: Work{OpenAlexID: "W2741809807"},
			want: "[not fetched: W2741809807]",
		},
		{
			name: "unresolved",
			work: Work{OpenAlexID: "W999", Unresolved: true},
			want: "[not in OpenAlex: W999]",
		},
		{
			// A hydrated work really can come back with a null title: OpenAlex
			// holds records for retractions and errata that carry nothing else.
			name: "hydrated but titleless falls back rather than returning empty",
			work: Work{OpenAlexID: "W3", Hydrated: true},
			want: "[not fetched: W3]",
		},
		{
			name: "empty string title is treated as absent",
			work: Work{OpenAlexID: "W4", Hydrated: true, Title: Ptr("")},
			want: "[not fetched: W4]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.work.DisplayTitle(); got != tt.want {
				t.Errorf("DisplayTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

// DisplayTitle exists so that no output path can print a blank where a work
// should be. That is the property worth asserting, separately from the exact
// wording above.
func TestDisplayTitleIsNeverEmpty(t *testing.T) {
	t.Parallel()
	works := []Work{
		{OpenAlexID: "W1"},
		{OpenAlexID: "W1", Hydrated: true},
		{OpenAlexID: "W1", Unresolved: true},
		{OpenAlexID: "W1", Hydrated: true, Title: Ptr("")},
	}
	for _, w := range works {
		if w.DisplayTitle() == "" {
			t.Errorf("DisplayTitle() returned empty for %+v", w)
		}
	}
}

func TestHydratedWorkIsDeadEnd(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		work HydratedWork
		want bool
	}{
		{
			name: "references fetched and empty",
			work: HydratedWork{Work: Work{OpenAlexID: "W1", Hydrated: true, FetchedRefs: true}},
			want: true,
		},
		{
			name: "references fetched and present",
			work: HydratedWork{
				Work:            Work{OpenAlexID: "W1", Hydrated: true, FetchedRefs: true},
				ReferencedWorks: []string{"W2"},
			},
			want: false,
		},
		{
			// The distinction the flag exists for: not yet asked is not the
			// same as asked and told none.
			name: "not yet fetched is not a dead end",
			work: HydratedWork{Work: Work{OpenAlexID: "W1", Hydrated: true}},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.work.IsDeadEnd(); got != tt.want {
				t.Errorf("IsDeadEnd() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The reason IsDeadEnd lives on HydratedWork and not on Work: a Work read back
// from the store has FetchedRefs set and no reference list to consult, so the
// same test on Work would call every persisted work a dead end. This asserts the
// type split, and it stops compiling if anyone moves the method back.
func TestDeadEndIsNotAskableOfAStoredWork(t *testing.T) {
	t.Parallel()
	stored := Work{OpenAlexID: "W1", Hydrated: true, FetchedRefs: true}
	if _, ok := any(&stored).(interface{ IsDeadEnd() bool }); ok {
		t.Error("Work must not expose IsDeadEnd: a stored Work has no reference list " +
			"to answer with, so every persisted work would report as a dead end")
	}
}

func TestAuthorsLoaded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		work Work
		want bool
	}{
		{"nil means the join was never run", Work{OpenAlexID: "W1"}, false},
		{"empty non-nil means loaded and genuinely authorless", Work{OpenAlexID: "W1", Authors: []Author{}}, true},
		{"populated", Work{OpenAlexID: "W1", Authors: []Author{{Name: "A"}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.work.AuthorsLoaded(); got != tt.want {
				t.Errorf("AuthorsLoaded() = %v, want %v", got, tt.want)
			}
		})
	}
}

// HydratedWork must forward the whole of Work, or every call site in the
// expansion loop needs a .Work in the middle of it.
func TestHydratedWorkEmbedsWork(t *testing.T) {
	t.Parallel()
	h := HydratedWork{
		Work:            Work{OpenAlexID: "W1", Hydrated: true, Title: Ptr("Filiation"), Depth: Ptr(0)},
		ReferencedWorks: []string{"W2", "W3"},
	}
	if h.DisplayTitle() != "Filiation" {
		t.Errorf("DisplayTitle() = %q, want %q", h.DisplayTitle(), "Filiation")
	}
	if h.IsStub() {
		t.Error("IsStub() = true for a hydrated work")
	}
	if Deref(h.Depth, -1) != 0 {
		t.Errorf("Depth = %d, want 0", Deref(h.Depth, -1))
	}
}

func TestOAStatusIsOpen(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status OAStatus
		want   bool
	}{
		{OAGold, true},
		{OAGreen, true},
		{OAHybrid, true},
		{OABronze, true},
		{OAClosed, false},
		{OAUnknown, false},
		{OAStatus("diamond"), false}, // unrecognised must not read as open
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			t.Parallel()
			if got := tt.status.IsOpen(); got != tt.want {
				t.Errorf("OAStatus(%q).IsOpen() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestExpansionResultReferenceCoverage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		result ExpansionResult
		want   float64
	}{
		{"empty run does not divide by zero", ExpansionResult{}, 0},
		{"negative hydrated is guarded", ExpansionResult{Hydrated: -1}, 0},
		{"full coverage", ExpansionResult{Hydrated: 50}, 1},
		{"medicine, ~6% dead ends", ExpansionResult{Hydrated: 100, DeadEnds: 6}, 0.94},
		{"arts and humanities, 84% dead ends", ExpansionResult{Hydrated: 100, DeadEnds: 84}, 0.16},
		{"every node a dead end", ExpansionResult{Hydrated: 10, DeadEnds: 10}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.result.ReferenceCoverage(); math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("ReferenceCoverage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeref(t *testing.T) {
	t.Parallel()
	if got := Deref[string](nil, "fallback"); got != "fallback" {
		t.Errorf("Deref(nil) = %q, want %q", got, "fallback")
	}
	if got := Deref(Ptr(1995), 0); got != 1995 {
		t.Errorf("Deref(Ptr(1995)) = %d, want 1995", got)
	}
	// The case the pointer discipline exists for: a real zero must survive.
	if got := Deref(Ptr(0), 42); got != 0 {
		t.Errorf("Deref(Ptr(0)) = %d, want 0 — a stored zero must not read as absent", got)
	}
}
