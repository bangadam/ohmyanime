package app

// reading_progress_test.go covers issue #9's HTTP surface: GET/PUT of the
// manga reading progress with optimistic-concurrency and clamping semantics.
// All data is seeded straight into the store, so no provider is contacted.

import (
	"context"
	"testing"

	"myanime/internal/domain"
	"myanime/internal/store"
)

func TestHTTPMangaReadingProgressContract(t *testing.T) {
	c := newContractClient(t)
	ctx := context.Background()
	if err := c.app.db.UpsertManga(ctx, domain.Manga{ID: "mangadex:m", Title: "Example", Availability: []string{"mangadex"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.app.db.UpsertChapters(ctx, []domain.Chapter{{ID: "chapter-1", MangaID: "mangadex:m", Label: "Ch. 1", Ordinal: 1, PageCount: 3}}); err != nil {
		t.Fatal(err)
	}

	// No progress yet: the API reports null so the reader starts fresh.
	if got := string(c.request("GET", "/manga/mangadex:m/reading-progress", "", 200).Data); got != "null" {
		t.Fatalf("missing progress = %s", got)
	}

	// First save with expectedRevision 0 inserts at revision 1.
	p := contractData[store.ReadingProgressState](t, c.request("PUT", "/manga/mangadex:m/reading-progress", `{"chapterId":"chapter-1","page":2,"completed":true,"expectedRevision":0}`, 200))
	if p.Revision != 1 || p.Page != 2 || !p.Completed || p.ChapterID != "chapter-1" || p.MangaID != "mangadex:m" {
		t.Fatalf("insert: %+v", p)
	}

	// A second window still holding expectedRevision 0 is rejected with 409.
	c.request("PUT", "/manga/mangadex:m/reading-progress", `{"chapterId":"chapter-1","page":0,"completed":false,"expectedRevision":0}`, 409)
	kept := contractData[store.ReadingProgressState](t, c.request("GET", "/manga/mangadex:m/reading-progress", "", 200))
	if kept.Page != 2 || kept.Revision != 1 || !kept.Completed {
		t.Fatalf("stale write mutated state: %+v", kept)
	}

	// An out-of-range page clamps to the chapter's stored page count.
	p = contractData[store.ReadingProgressState](t, c.request("PUT", "/manga/mangadex:m/reading-progress", `{"chapterId":"chapter-1","page":9,"completed":false,"expectedRevision":1}`, 200))
	if p.Page != 2 || p.Revision != 2 {
		t.Fatalf("clamp: %+v", p)
	}

	// Invalid payloads: unknown chapter, negative page, unknown field.
	for _, body := range []string{
		`{"chapterId":"missing","page":0,"expectedRevision":2}`,
		`{"chapterId":"chapter-1","page":-1,"expectedRevision":2}`,
		`{"chapterId":"chapter-1","page":0,"expectedRevision":2,"unknown":true}`,
	} {
		c.request("PUT", "/manga/mangadex:m/reading-progress", body, 400)
	}
	saved := contractData[store.ReadingProgressState](t, c.request("GET", "/manga/mangadex:m/reading-progress", "", 200))
	if saved.Revision != 2 || saved.Page != 2 {
		t.Fatalf("failed writes mutated state: %+v", saved)
	}

	// Writing progress for an unknown manga is rejected too.
	c.request("PUT", "/manga/mangadex:missing/reading-progress", `{"chapterId":"chapter-1","page":0,"expectedRevision":0}`, 400)
}
