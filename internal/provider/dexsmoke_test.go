package provider

import (
	"context"
	"os"
	"testing"
)

func TestDexSmoke(t *testing.T) {
	if os.Getenv("DEX_SMOKE_NETWORK") == "" {
		t.Skip("live mangadex smoke test; set DEX_SMOKE_NETWORK=1 to run")
	}
	s := New()
	ctx := context.Background()
	manga, next, e := s.MangaSearch(ctx, "one piece", "", 5)
	if e != nil {
		t.Fatal(e)
	}
	if len(manga) == 0 {
		t.Fatal("no results")
	}
	t.Logf("first=%s poster=%s next=%q type=%s year=%d author=%s tags=%d", manga[0].Title, manga[0].PosterURL, next, manga[0].Type, manga[0].Year, manga[0].Author, len(manga[0].Tags))
	d, e := s.MangaDetail(ctx, manga[0].ID)
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("detail=%s alt=%d synopsis=%d chars", d.Title, len(d.AlternativeTitles), len(d.Synopsis))
	chs, e := s.MangaChapters(ctx, "mangadex:f9c33607-9180-4ba6-b85c-e4b5faee7192")
	if e != nil {
		t.Fatal(e)
	}
	if len(chs) == 0 {
		t.Fatal("no chapters")
	}
	last := chs[len(chs)-1]
	t.Logf("chapters=%d last=%q number=%q vol=%q special=%q pages=%d group=%s", len(chs), last.Label, last.Number, last.Volume, last.Special, last.PageCount, last.GroupName)
	pages, e := s.MangaPages(ctx, last.ID)
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("pages=%d first=%s", len(pages), pages[0].URL)
	ch, e := s.MangaChapter(ctx, last.ID)
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("single chapter=%q mangaId=%s", ch.Label, ch.MangaID)
}
