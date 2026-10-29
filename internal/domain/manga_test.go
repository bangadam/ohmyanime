package domain

import (
	"reflect"
	"testing"
)

func TestChapterKeyDistinctAndStable(t *testing.T) {
	base := ChapterKey{MangaID: "m", Language: "en", Volume: "1", Number: "12"}
	cases := []struct {
		name string
		a, b ChapterKey
	}{
		{"same key", base, base},
		{"decimal chapter", base, ChapterKey{MangaID: "m", Language: "en", Volume: "1", Number: "12.5"}},
		{"special", base, ChapterKey{MangaID: "m", Language: "en", Volume: "1", Number: "12", Special: "ova"}},
		{"language", base, ChapterKey{MangaID: "m", Language: "ja", Volume: "1", Number: "12"}},
		{"volume", base, ChapterKey{MangaID: "m", Language: "en", Volume: "2", Number: "12"}},
	}
	for _, c := range cases {
		if got := c.a.ID(); got == "" {
			t.Fatalf("%s: empty id", c.name)
		}
		same := c.a.ID() == c.b.ID()
		if (c.name == "same key") != same {
			t.Errorf("%s: distinctness wrong (same=%v, %q vs %q)", c.name, same, c.a.ID(), c.b.ID())
		}
		if c.a.ID() != (ChapterKey{MangaID: c.a.MangaID, Language: c.a.Language, Volume: c.a.Volume, Number: c.a.Number, Special: c.a.Special}).ID() {
			t.Errorf("%s: id not stable", c.name)
		}
	}
	if !base.Valid() {
		t.Error("valid key rejected")
	}
	if (ChapterKey{MangaID: "m"}).Valid() {
		t.Error("key without number or special accepted")
	}
}

func TestChapterKeyEscaping(t *testing.T) {
	a := ChapterKey{MangaID: "m:1", Number: "12:extra"}
	b := ChapterKey{MangaID: "m", Volume: "1", Number: "12", Special: "extra"}
	if a.ID() == b.ID() {
		t.Fatalf("separator collision: %q", a.ID())
	}
	if (ChapterKey{MangaID: "m\\c", Number: "1"}).ID() == (ChapterKey{MangaID: "m:c", Number: "1"}).ID() {
		t.Fatal("escape collision")
	}
}

func TestCompareChapterKeysOrdering(t *testing.T) {
	keys := []ChapterKey{
		{MangaID: "m", Language: "en", Volume: "1", Number: "2"},
		{MangaID: "m", Language: "en", Volume: "1", Number: "12"},
		{MangaID: "m", Language: "en", Volume: "1", Number: "12.5"},
		{MangaID: "m", Language: "en", Volume: "1", Special: "extra"},
	}
	for i := 1; i < len(keys); i++ {
		if CompareChapterKeys(keys[i-1], keys[i]) >= 0 {
			t.Fatalf("order broken at %d: %+v vs %+v", i, keys[i-1], keys[i])
		}
	}
	for _, k := range keys {
		if CompareChapterKeys(k, k) != 0 {
			t.Fatalf("identical keys not equal: %+v", k)
		}
	}
	// Number strings stay distinct even when numerically equal.
	if CompareChapterKeys(ChapterKey{Number: "12"}, ChapterKey{Number: "12.0"}) == 0 {
		t.Fatal("12 and 12.0 collapsed")
	}
}

func TestSortChaptersDeterministic(t *testing.T) {
	mk := func(number, special string, ordinal float64) Chapter {
		return Chapter{ID: ChapterKey{MangaID: "m", Language: "en", Volume: "1", Number: number, Special: special}.ID(), MangaID: "m", Label: number + special, Number: number, Special: special, Ordinal: ordinal}
	}
	chapters := []Chapter{mk("12", "", 12), mk("2", "", 2), mk("12.5", "", 12.5), mk("", "extra", 1e9), mk("10", "", 10)}
	SortChapters(chapters)
	got := []string{}
	for _, c := range chapters {
		got = append(got, c.Label)
	}
	want := []string{"2", "10", "12", "12.5", "extra"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order=%v want=%v", got, want)
	}
	// Re-sorting a copy yields the same order.
	again := append([]Chapter(nil), chapters...)
	SortChapters(again)
	for i := range again {
		if again[i].ID != chapters[i].ID {
			t.Fatalf("unstable at %d", i)
		}
	}
}
