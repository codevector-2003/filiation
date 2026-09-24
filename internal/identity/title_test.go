package identity

import "testing"

func TestTitleKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
	}{
		{"case and spacing", "Attention  Is All You Need", "attentionisallyouneed"},
		{"punctuation", "BERT: Pre-training of Deep Bidirectional Transformers",
			"bertpretrainingofdeepbidirectionaltransformers"},
		{"dash styles agree", "Self–attention — revisited", "selfattentionrevisited"},
		{"digits kept", "GPT-4 Technical Report", "gpt4technicalreport"},
		{"non-latin letters kept", "Über die Möglichkeit", "überdiemöglichkeit"},
		{"nothing left", "?!. —", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := TitleKey(tt.in); got != tt.want {
				t.Errorf("TitleKey(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTitlesMatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"identical", "Attention Is All You Need", "Attention Is All You Need", true},
		{"case and punctuation only", "attention is all you need.", "Attention Is All You Need", true},
		{"one typo", "Attention Is All You Ned", "Attention Is All You Need", true},
		{"hyphenation", "Pre-training of deep bidirectional transformers",
			"Pretraining of Deep Bidirectional Transformers", true},

		// The cases a looser test gets wrong, and ADR-005 exists for.
		{"prefix is not the paper", "Attention", "Attention Is All You Need", false},
		{"missing subtitle", "Deep Residual Learning",
			"Deep Residual Learning for Image Recognition", false},
		{"different paper", "Attention Is All You Need", "Attention Is Not Explanation", false},
		{"empty", "", "Attention Is All You Need", false},
		{"both empty", "", "", false},
		{"punctuation only", "?!", "?!", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := TitlesMatch(tt.a, tt.b); got != tt.want {
				t.Errorf("TitlesMatch(%q, %q) = %v (similarity %.2f), want %v",
					tt.a, tt.b, got, TitleSimilarity(tt.a, tt.b), tt.want)
			}
			if TitlesMatch(tt.a, tt.b) != TitlesMatch(tt.b, tt.a) {
				t.Errorf("TitlesMatch is not symmetric for %q, %q", tt.a, tt.b)
			}
		})
	}
}

func TestTitleSimilarityBounds(t *testing.T) {
	t.Parallel()
	if got := TitleSimilarity("abc", "abc"); got != 1 {
		t.Errorf("TitleSimilarity of identical titles = %v, want 1", got)
	}
	if got := TitleSimilarity("abc", "xyz"); got != 0 {
		t.Errorf("TitleSimilarity with nothing in common = %v, want 0", got)
	}
	if got := TitleSimilarity("", ""); got != 0 {
		t.Errorf("TitleSimilarity of two empty titles = %v, want 0", got)
	}
}
