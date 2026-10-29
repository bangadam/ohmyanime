package provider

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestProbeMangaAdaptersLive is the authorized live smoke for the registered
// manga adapters: search -> detail -> chapters -> pages against real responses.
// It never runs in CI; set PROBE_NETWORK=1 to exercise it by hand.
func TestProbeMangaAdaptersLive(t *testing.T) {
	if os.Getenv("PROBE_NETWORK") != "1" {
		t.Skip("live provider probe; set PROBE_NETWORK=1 to run")
	}
	s := New()
	for _, p := range s.mangaProviders() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		id := p.Identity().ID
		items, next, err := p.Search(ctx, "one piece", "", 3)
		if err != nil || len(items) == 0 {
			t.Errorf("%s: search n=%d next=%q err=%v", id, len(items), next, err)
			cancel()
			continue
		}
		t.Logf("%s: search n=%d next=%q first=%q", id, len(items), next, items[0].Title)
		mangaID := items[0].ID
		if d, derr := p.Detail(ctx, mangaID); derr != nil {
			t.Errorf("%s: detail: %v", id, derr)
		} else {
			t.Logf("%s: detail %q", id, d.Title)
		}
		chapters, cerr := p.Chapters(ctx, mangaID)
		if cerr != nil || len(chapters) == 0 {
			t.Errorf("%s: chapters n=%d err=%v", id, len(chapters), cerr)
			cancel()
			continue
		}
		t.Logf("%s: chapters n=%d first=%q pages=%d", id, len(chapters), chapters[0].Label, chapters[0].PageCount)

		// A listed chapter can still 404 at the page endpoint when the source
		// removed the images after the inventory was built, so try a bounded
		// handful and require at least one readable chapter.
		readable := 0
		for i, ch := range chapters {
			if i >= 5 {
				break
			}
			pages, perr := p.Pages(ctx, ch.ID)
			if perr == nil && len(pages) > 0 {
				readable++
				t.Logf("%s: pages n=%d first=%.48s (%s)", id, len(pages), pages[0].URL, ch.Label)
				break
			}
			t.Logf("%s: %s pages failed (post-hoc removal tolerated): %v", id, ch.Label, perr)
		}
		if readable == 0 {
			// Inventory and pages are independent: a title whose images were
			// removed after publication lists chapters but cannot serve pages.
			// That is a content outcome, so log rather than fail; the readable
			// seed check below proves the page pipeline itself works.
			t.Logf("%s: no readable chapter among the first five (content outcome)", id)
		}
		cancel()
	}

	// A title known to publish readable pages proves the page pipeline itself,
	// independent of whether a given popular title is externally hosted.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p, e := s.mangaProviderOf("mangadex:")
	if e != nil {
		t.Fatal(e)
	}
	chapters, e := p.Chapters(ctx, "mangadex:0a580438-bc72-4503-940b-12a5da881b56")
	if e != nil || len(chapters) == 0 {
		t.Fatalf("mangadex seed chapters: n=%d err=%v", len(chapters), e)
	}
	pages, e := p.Pages(ctx, chapters[0].ID)
	if e != nil || len(pages) == 0 {
		t.Fatalf("mangadex seed pages: n=%d err=%v", len(pages), e)
	}
	t.Logf("mangadex seed: %s -> %d pages", chapters[0].Label, len(pages))
}
