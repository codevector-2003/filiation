package blobs

import (
	"path"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/codevector-2003/filiation/internal/identity"
	"github.com/codevector-2003/filiation/internal/model"
)

const (
	// maxTitleRunes is where the title part of a folder name stops. With the
	// author, year and fil ID that keeps a name near 90 characters, so a full
	// path stays well under Windows' 260 even several folders deep.
	maxTitleRunes = 60

	// LongPath is the length past which fil warns that a path is close to
	// Windows' 260-character limit, which some tools still enforce.
	LongPath = 240

	// idSeparator ends the readable part of a work folder's name. Two dashes,
	// because a single one already separates words.
	idSeparator = "--"
)

// FolderName is the name fil gives a work's folder:
// {FirstAuthorSurname}-{Year}-{ShortTitle}--{filID}.
//
// Accents are folded to ASCII and everything but letters and digits is dropped,
// so no character Windows forbids can survive, and a name ending in a fil ID
// is never a reserved one like CON or NUL. A work with no author gives
// Unknown, with no year nd, and with no title — or one in a script with no
// ASCII letters — Untitled. The fil ID at the end keeps names unique on
// case-insensitive file systems and lets a scan find a folder the researcher
// moved.
func FolderName(w model.Work) string {
	author := "Unknown"
	if len(w.Authors) > 0 {
		if s := words(surname(w.Authors[0].Name), 0); s != "" {
			author = s
		}
	}
	year := "nd"
	if w.Year != nil && *w.Year > 0 {
		year = strconv.Itoa(*w.Year)
	}
	title := ""
	if w.Title != nil {
		title = words(*w.Title, maxTitleRunes)
	}
	if title == "" {
		title = "Untitled"
	}
	return author + "-" + year + "-" + title + idSeparator + w.FilID
}

// FolderID returns the fil ID at the end of a folder's name, normalised, and
// whether there is one. It accepts any letter case, because a person may retype
// a name and some file systems do not keep case.
func FolderID(name string) (string, bool) {
	i := strings.LastIndex(name, idSeparator)
	if i < 0 {
		return "", false
	}
	id, err := identity.NormaliseFilID(name[i+len(idSeparator):])
	if err != nil {
		return "", false
	}
	return id, true
}

// surname picks the family name out of an author's display name: the part
// before a comma ("Piwowar, Heather"), otherwise the last word ("Heather
// Piwowar"). OpenAlex sends the second form; the first is what people type.
func surname(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.Index(name, ","); i > 0 {
		return name[:i]
	}
	f := strings.Fields(name)
	if len(f) == 0 {
		return ""
	}
	return f[len(f)-1]
}

// words folds s to ASCII letters and digits joined by single dashes, stopping
// at the last whole word within limit runes (0 means no limit). A single word
// longer than the limit is cut.
func words(s string, limit int) string {
	var b strings.Builder
	pendingDash := false
	for _, r := range fold(s) {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingDash = false
			b.WriteRune(r)
			continue
		}
		pendingDash = true
	}
	out := b.String()
	if limit <= 0 || len(out) <= limit {
		return out
	}
	cut := out[:limit]
	if out[limit] != '-' {
		if i := strings.LastIndexByte(cut, '-'); i > 0 {
			cut = cut[:i]
		}
	}
	return strings.TrimRight(cut, "-")
}

// letters that do not decompose into a base letter and an accent, so
// normalisation alone would drop them.
var foldLetters = strings.NewReplacer(
	"ß", "ss", "Æ", "AE", "æ", "ae", "Œ", "OE", "œ", "oe", "Ø", "O", "ø", "o",
	"Ł", "L", "ł", "l", "Đ", "D", "đ", "d", "Ð", "D", "ð", "d", "Þ", "Th", "þ", "th",
	"ı", "i", "Ħ", "H", "ħ", "h",
)

// fold removes accents: é becomes e, ß becomes ss. It decomposes each letter
// into its base and combining marks, then drops the marks.
func fold(s string) string {
	s = norm.NFD.String(foldLetters.Replace(s))
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Clean turns a path relative to the library folder into the form the database
// stores: forward slashes, no leading ./, and Unicode NFC. macOS stores accented
// names decomposed, so without NFC the same folder would compare unequal on
// different machines.
func Clean(rel string) string {
	rel = strings.ReplaceAll(rel, `\`, "/")
	rel = path.Clean(rel)
	return norm.NFC.String(strings.TrimPrefix(rel, "./"))
}

// FileVersion maps a file name fil gave to the version of the paper it holds,
// in OpenAlex's vocabulary, or "" for any other name.
func FileVersion(name string) string {
	switch strings.ToLower(name) {
	case "published.pdf":
		return "published"
	case "accepted.pdf":
		return "accepted"
	case "preprint.pdf":
		return "preprint"
	}
	return ""
}
