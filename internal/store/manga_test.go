package store

import (
	"context"
	"path/filepath"
	"testing"

	"myanime/internal/domain"
)

// TestMangaMigrationPreservesAnimeAndIsIdempotent covers issue #5: migration
// version 3 must be additive, preserve anime bookmarks and progress, keep
// chapters 12, 12.5 and specials distinct, order deterministically, and be
// safe to rerun (reopen).
func TestMangaMigrationPreservesAnimeAndIsIdempotent(t *testing.T) {
	s, path := fixture(t)
	ctx := context.Background()

	// Anime state written before the manga schema exists.
	duration := 100.0
	if _, err := s.PutProgress(ctx, "ep", ProgressUpdate{PositionSeconds: 42, DurationSeconds: &duration, Mode: "sub", Provider: "hianime"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutBookmark(ctx, "anime"); err != nil {
		t.Fatal(err)
	}

	// Manga library content using identity-preserving chapter IDs.
	manga := domain.Manga{ID: "mangadex:m", Title: "Example", Availability: []string{"mangadex"}}
	if err := s.UpsertManga(ctx, manga); err != nil {
		t.Fatal(err)
	}
	key := func(number, special string) domain.ChapterKey {
		return domain.ChapterKey{MangaID: "mangadex:m", Language: "en", Volume: "1", Number: number, Special: special}
	}
	chapters := []domain.Chapter{
		{ID: key("12.5", "").ID(), MangaID: manga.ID, Label: "Ch. 12.5", Ordinal: 12.5, Number: "12.5", Language: "en"},
		{ID: key("12", "").ID(), MangaID: manga.ID, Label: "Ch. 12", Ordinal: 12, Number: "12", Language: "en"},
		{ID: key("", "extra").ID(), MangaID: manga.ID, Label: "Extra", Ordinal: 1e9, Special: "extra", Language: "en"},
		{ID: key("2", "").ID(), MangaID: manga.ID, Label: "Ch. 2", Ordinal: 2, Number: "2", Language: "en"},
	}
	if err := s.UpsertChapters(ctx, chapters); err != nil {
		t.Fatal(err)
	}

	// 12, 12.5 and the special remain distinct rows.
	ids := map[string]bool{}
	for _, c := range chapters {
		ids[c.ID] = true
	}
	if len(ids) != len(chapters) {
		t.Fatalf("chapter identity collapsed: %d unique of %d", len(ids), len(chapters))
	}

	// Reading progress on a distinct chapter.
	if _, err := s.PutReadingProgress(ctx, manga.ID, ReadingProgressUpdate{ChapterID: chapters[1].ID, Page: 7}); err != nil {
		t.Fatal(err)
	}

	// Reopening reruns migrations against an already-migrated database.
	s.Close()
	reopened, err := Open(filepath.Join(filepath.Dir(path), "reopen.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.UpsertManga(ctx, manga); err != nil {
		t.Fatalf("reopen as fresh db: %v", err)
	}

	// Original database: reopen the same file and verify everything survived.
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()

	got, err := again.Anime(ctx, "anime")
	if err != nil || got.Title != "Example" {
		t.Fatalf("anime lost: %+v err=%v", got, err)
	}
	prog, err := again.Progress(ctx, "ep")
	if err != nil || prog.PositionSeconds != 42 {
		t.Fatalf("watch progress lost: %+v err=%v", prog, err)
	}
	bm, err := again.Bookmarks(ctx, 10, "")
	if err != nil || len(bm.Items) != 1 || bm.Items[0].ID != "anime" {
		t.Fatalf("bookmarks lost: %+v err=%v", bm, err)
	}

	stored, err := again.Chapters(ctx, manga.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Deterministic ordering: 2 < 12 < 12.5 < special.
	want := []string{"Ch. 2", "Ch. 12", "Ch. 12.5", "Extra"}
	if len(stored) != len(want) {
		t.Fatalf("stored=%d want=%d", len(stored), len(want))
	}
	for i, c := range stored {
		if c.Label != want[i] {
			t.Fatalf("order[%d]=%q want=%q", i, c.Label, want[i])
		}
	}

	// Upserting the same chapters again is a no-op that preserves rows.
	if err := again.UpsertChapters(ctx, chapters); err != nil {
		t.Fatal(err)
	}
	reread, err := again.Chapters(ctx, manga.ID)
	if err != nil || len(reread) != len(chapters) {
		t.Fatalf("rerun upsert changed rows: %d err=%v", len(reread), err)
	}

	rp, err := again.ReadingProgress(ctx, manga.ID)
	if err != nil || rp.ChapterID != chapters[1].ID || rp.Page != 7 || rp.Revision != 1 {
		t.Fatalf("reading progress lost: %+v err=%v", rp, err)
	}

	// A second progress write advances the revision instead of duplicating.
	if _, err := again.PutReadingProgress(ctx, manga.ID, ReadingProgressUpdate{ChapterID: chapters[2].ID, Page: 0, Completed: true}); err != nil {
		t.Fatal(err)
	}
	rp2, err := again.ReadingProgress(ctx, manga.ID)
	if err != nil || !rp2.Completed || rp2.Revision != 2 {
		t.Fatalf("reading progress update wrong: %+v err=%v", rp2, err)
	}
}

// TestMangaForeignKeysAndValidation exercises the schema's referential rules.
func TestMangaForeignKeysAndValidation(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()

	if err := s.UpsertManga(ctx, domain.Manga{ID: "", Title: "x"}); err != ErrInvalid {
		t.Fatalf("invalid manga accepted: %v", err)
	}
	if err := s.UpsertChapters(ctx, []domain.Chapter{{ID: "c", MangaID: "missing", Label: "Ch. 1"}}); err == nil {
		t.Fatal("chapter for unknown manga accepted")
	}
	if err := s.UpsertManga(ctx, domain.Manga{ID: "m", Title: "Example"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertChapters(ctx, []domain.Chapter{{ID: "c", MangaID: "m", Label: "Ch. 1", Ordinal: 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutReadingProgress(ctx, "m", ReadingProgressUpdate{ChapterID: "missing"}); err == nil {
		t.Fatal("progress for unknown chapter accepted")
	}
	if _, err := s.PutReadingProgress(ctx, "m", ReadingProgressUpdate{ChapterID: "c", Page: -1}); err != ErrInvalid {
		t.Fatalf("negative page accepted: %v", err)
	}
}

// TestReadingProgressRevisionAndClamp covers issue #9: a stale window must
// never overwrite newer progress, and a shrunken page inventory must clamp
// the saved logical page instead of persisting an out-of-range index.
func TestReadingProgressRevisionAndClamp(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	if err := s.UpsertManga(ctx, domain.Manga{ID: "m", Title: "Example"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertChapters(ctx, []domain.Chapter{{ID: "c", MangaID: "m", Label: "Ch. 1", Ordinal: 1, PageCount: 3}}); err != nil {
		t.Fatal(err)
	}

	// First write: expected revision 0 against an absent row inserts.
	zero := int64(0)
	state, err := s.PutReadingProgress(ctx, "m", ReadingProgressUpdate{ChapterID: "c", Page: 2, ExpectedRevision: &zero})
	if err != nil || state.Revision != 1 || state.Page != 2 {
		t.Fatalf("insert: %+v err=%v", state, err)
	}

	// A second window that still believes revision 0 is rejected: the row
	// now sits at revision 1.
	if _, err := s.PutReadingProgress(ctx, "m", ReadingProgressUpdate{ChapterID: "c", Page: 0, ExpectedRevision: &zero}); err != ErrConflict {
		t.Fatalf("stale revision accepted: %v", err)
	}
	kept, _ := s.ReadingProgress(ctx, "m")
	if kept.Page != 2 || kept.Revision != 1 {
		t.Fatalf("stale write mutated state: %+v", kept)
	}

	// An up-to-date write succeeds and bumps the revision.
	one := state.Revision
	state, err = s.PutReadingProgress(ctx, "m", ReadingProgressUpdate{ChapterID: "c", Page: 9, ExpectedRevision: &one})
	if err != nil || state.Revision != 2 {
		t.Fatalf("current write rejected: %+v err=%v", state, err)
	}
	// Page 9 in a 3-page chapter clamps to the last page (index 2).
	if state.Page != 2 {
		t.Fatalf("clamp: page=%d want 2", state.Page)
	}

	// The inventory shrinks to 2 pages: the next save clamps again.
	if err := s.UpsertChapters(ctx, []domain.Chapter{{ID: "c", MangaID: "m", Label: "Ch. 1", Ordinal: 1, PageCount: 2}}); err != nil {
		t.Fatal(err)
	}
	two := state.Revision
	state, err = s.PutReadingProgress(ctx, "m", ReadingProgressUpdate{ChapterID: "c", Page: 2, ExpectedRevision: &two})
	if err != nil || state.Page != 1 || state.Revision != 3 {
		t.Fatalf("reclamp: %+v err=%v", state, err)
	}
}

// TestMangaMigrationUpgradesVersion2Database simulates an installation last
// opened before migration3 existed: the manga tables and its schema_migrations
// row are removed while anime data stays, then the upgrade path must recreate
// them without touching anime rows.
func TestMangaMigrationUpgradesVersion2Database(t *testing.T) {
	s, path := fixture(t)
	ctx := context.Background()
	duration := 100.0
	if _, err := s.PutProgress(ctx, "ep", ProgressUpdate{PositionSeconds: 42, DurationSeconds: &duration, Mode: "sub", Provider: "hianime"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutBookmark(ctx, "anime"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"DROP TABLE reading_progress",
		"DROP TABLE chapter_provider_mappings",
		"DROP TABLE manga_provider_mappings",
		"DROP TABLE manga_bookmarks",
		"DROP TABLE chapters",
		"DROP TABLE manga",
		"DROP TABLE manual_watched",
		"DELETE FROM schema_migrations WHERE version>=3",
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()

	upgraded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	if _, err := upgraded.Progress(ctx, "ep"); err != nil {
		t.Fatalf("watch progress lost on upgrade: %v", err)
	}
	bm, err := upgraded.Bookmarks(ctx, 10, "")
	if err != nil || len(bm.Items) != 1 {
		t.Fatalf("bookmarks lost on upgrade: %+v err=%v", bm, err)
	}
	// Manga tables are live again and accept new rows.
	if err := upgraded.UpsertManga(ctx, domain.Manga{ID: "m", Title: "Example"}); err != nil {
		t.Fatal(err)
	}
	if _, err := upgraded.PutReadingProgress(ctx, "m", ReadingProgressUpdate{ChapterID: "c"}); err == nil {
		t.Fatal("reading progress for unknown chapter accepted")
	}
}
