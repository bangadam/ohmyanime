package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// ChapterKey captures every identity dimension of a chapter release. Two
// chapters are the same chapter only when all of these match: language,
// volume, the decimal chapter number ("12" vs "12.5") and any special
// distinction ("ova", "extra", ...). Empty fields are distinct from set ones.
type ChapterKey struct {
	MangaID  string
	Language string
	Volume   string
	Number   string
	Special  string
}

// Escape keeps the composite ID unambiguous: a literal separator inside a
// field can never be confused with a field boundary.
func escapeKeyField(s string) string {
	replacer := strings.NewReplacer("\\", "\\\\", ":", "\\c")
	return replacer.Replace(strings.TrimSpace(s))
}

// ID returns the stable, deterministic identifier for the chapter key. The
// same release always maps to the same ID, and distinct releases never
// collide, without relying on a provider-assigned UUID.
func (k ChapterKey) ID() string {
	return fmt.Sprintf("manga-chapter:%s:%s:%s:%s:%s",
		escapeKeyField(k.MangaID), escapeKeyField(k.Language),
		escapeKeyField(k.Volume), escapeKeyField(k.Number), escapeKeyField(k.Special))
}

// Valid reports whether the key identifies a chapter at all. The manga and at
// least one of number or special must be present; a label-only entry is not a
// stable identity.
func (k ChapterKey) Valid() bool {
	return k.MangaID != "" && (k.Number != "" || k.Special != "")
}

// chapterSortNumber parses a decimal chapter number. Parse failures sort
// after every numeric chapter, keeping unparseable labels deterministic.
func chapterSortNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || n != n {
		return 0, false
	}
	return n, true
}

// CompareChapterKeys orders two chapter keys deterministically: by numeric
// chapter number ("2" < "12" < "12.5"), then volume, special and language as
// final tiebreakers so equal releases never swap positions. It returns -1
// when a sorts before b, +1 when after, and 0 only for identical keys.
func CompareChapterKeys(a, b ChapterKey) int {
	if c := compareNumberField(a.Number, b.Number); c != 0 {
		return c
	}
	if c := strings.Compare(a.Volume, b.Volume); c != 0 {
		return c
	}
	if c := strings.Compare(a.Special, b.Special); c != 0 {
		return c
	}
	return strings.Compare(a.Language, b.Language)
}

func compareNumberField(a, b string) int {
	na, aok := chapterSortNumber(a)
	nb, bok := chapterSortNumber(b)
	switch {
	case aok && bok:
		if na < nb {
			return -1
		} else if na > nb {
			return 1
		}
		return strings.Compare(a, b) // "12" vs "12.0" stay distinct but ordered
	case aok:
		return -1 // numeric chapters sort before unparseable labels
	case bok:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

// ReadingProgress is the resumable reading position for one manga. Progress
// is per manga, not per chapter: it points at the chapter last read and the
// page reached inside it.
type ReadingProgress struct {
	MangaID   string `json:"mangaId"`
	ChapterID string `json:"chapterId"`
	Page      int    `json:"page"`
	Completed bool   `json:"completed"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// SortChapters sorts chapters in place using the deterministic key ordering.
func SortChapters(chapters []Chapter) {
	// insertion sort keeps the sort stable and avoids importing sort for a
	// typically small chapter list
	for i := 1; i < len(chapters); i++ {
		for j := i; j > 0; j-- {
			if CompareChapterKeys(chapterKey(chapters[j-1]), chapterKey(chapters[j])) > 0 {
				chapters[j-1], chapters[j] = chapters[j], chapters[j-1]
			} else {
				break
			}
		}
	}
}

func chapterKey(c Chapter) ChapterKey {
	return ChapterKey{MangaID: c.MangaID, Language: c.Language, Volume: c.Volume, Number: c.Number, Special: c.Special}
}
