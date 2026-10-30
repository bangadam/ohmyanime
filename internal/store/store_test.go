package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"myanime/internal/domain"
)

func fixture(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	if err = s.UpsertAnime(ctx, domain.Anime{ID: "anime", Title: "Example"}); err != nil {
		t.Fatal(err)
	}
	if err = s.UpsertEpisodes(ctx, []domain.Episode{{ID: "ep", AnimeID: "anime", Label: "OVA-1", Ordinal: 1}}); err != nil {
		t.Fatal(err)
	}
	return s, path
}

// Genres and tags are part of the whole domain.Anime struct stored in
// anime.metadata_json, so they must survive a write/read round trip without a
// SQL column migration.
func TestAnimeMetadataRoundTripTaxonomy(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	want := domain.Anime{
		ID:           "taxonomy",
		Title:        "Taxonomy",
		Genres:       []string{"Action", "Fantasy"},
		Tags:         []string{"Isekai", "Time Travel"},
		Availability: []string{},
	}
	if err := s.UpsertAnime(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Anime(ctx, "taxonomy")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Genres, want.Genres) {
		t.Fatalf("genres = %v, want %v", got.Genres, want.Genres)
	}
	if !slices.Equal(got.Tags, want.Tags) {
		t.Fatalf("tags = %v, want %v", got.Tags, want.Tags)
	}
}
func TestContinueHiddenHideUnhidePersistence(t *testing.T) {
	s, path := fixture(t)
	ctx := context.Background()
	if err := s.UpsertEpisodes(ctx, []domain.Episode{{ID: "ep2", AnimeID: "anime", Label: "OVA-2", Ordinal: 2}}); err != nil {
		t.Fatal(err)
	}
	duration := 100.0
	if _, err := s.PutProgress(ctx, "ep", ProgressUpdate{PositionSeconds: 10, DurationSeconds: &duration, Mode: "sub", Provider: "hianime"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutProgress(ctx, "ep2", ProgressUpdate{PositionSeconds: 5, DurationSeconds: &duration, Mode: "sub", Provider: "hianime"}); err != nil {
		t.Fatal(err)
	}
	if err := s.HideContinue(ctx, "anime"); err != nil {
		t.Fatal(err)
	}
	if err := s.HideContinue(ctx, "anime"); err != nil {
		t.Fatal(err) // idempotent
	}
	// Persist across restart.
	s.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	ids, err := reopened.ContinueHidden(ctx)
	if err != nil || len(ids) != 1 || ids[0] != "anime" {
		t.Fatalf("hidden=%v err=%v", ids, err)
	}
	// History entries and playback positions remain intact.
	page, err := reopened.History(ctx, 10, "")
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("history=%+v err=%v", page, err)
	}
	for _, item := range page.Items {
		if got := item.Progress.PositionSeconds; got != 10 && got != 5 {
			t.Fatalf("position lost: %+v", item.Progress)
		}
	}
	// Watching the title again un-hides it.
	if _, err = reopened.PutProgress(ctx, "ep", ProgressUpdate{PositionSeconds: 20, DurationSeconds: &duration, Mode: "sub", Revision: 1, Provider: "hianime"}); err != nil {
		t.Fatal(err)
	}
	if ids, err = reopened.ContinueHidden(ctx); err != nil || len(ids) != 0 {
		t.Fatalf("hidden after rewatch=%v err=%v", ids, err)
	}
	if err = reopened.HideContinue(ctx, ""); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
func TestDurabilityCompletionAndSeparation(t *testing.T) {
	s, path := fixture(t)
	ctx := context.Background()
	if err := s.PutBookmark(ctx, "anime"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutBookmark(ctx, "anime"); err != nil {
		t.Fatal(err)
	}
	duration := 100.0
	p, err := s.PutProgress(ctx, "ep", ProgressUpdate{PositionSeconds: 95, DurationSeconds: &duration, Mode: "sub", Provider: "hianime"})
	if err != nil || !p.Completed || p.Revision != 1 {
		t.Fatalf("progress=%+v err=%v", p, err)
	}
	p, err = s.PutProgress(ctx, "ep", ProgressUpdate{PositionSeconds: 2, DurationSeconds: &duration, Mode: "sub", Revision: 1})
	if err != nil || !p.Completed {
		t.Fatalf("completion must remain sticky: %+v %v", p, err)
	}
	if _, err = s.PutProgress(ctx, "ep", ProgressUpdate{PositionSeconds: 4, Mode: "sub", Revision: 1}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update: %v", err)
	}
	if err = s.PutCache(ctx, "catalog", CacheEntry{Payload: json.RawMessage(`{"title":"Example"}`), ExpiresAt: time.Now().Add(time.Hour), SchemaVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if err = s.ClearCache(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Cache(ctx, "catalog", 1); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	h, err := s.History(ctx, 20, "")
	if err != nil || len(h.Items) != 1 || h.Items[0].Provider != "hianime" {
		t.Fatalf("history=%+v err=%v", h, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err = s.Progress(ctx, "ep")
	if err != nil || p.Revision != 2 || p.PositionSeconds != 2 {
		t.Fatalf("restart=%+v err=%v", p, err)
	}
	p, err = s.ResetProgress(ctx, "ep", 2)
	if err != nil || p.Completed || p.Revision != 3 {
		t.Fatalf("reset=%+v err=%v", p, err)
	}
	if err = s.ClearHistory(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Progress(ctx, "ep"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	b, err := s.Bookmarks(ctx, 20, "")
	if err != nil || len(b.Items) != 1 {
		t.Fatalf("bookmarks=%+v err=%v", b, err)
	}
	if err = s.ClearBookmarks(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Anime(ctx, "anime"); err != nil {
		t.Fatal(err)
	}
}
func TestSettingsRevisionValidationAndReset(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	v, err := s.Settings(ctx)
	if err != nil || v.Autoplay || v.Revision != 0 {
		t.Fatalf("defaults=%+v err=%v", v, err)
	}
	v, err = s.PatchSettings(ctx, 0, map[string]json.RawMessage{"autoplay": json.RawMessage("true")})
	if err != nil || !v.Autoplay || v.Revision != 1 {
		t.Fatalf("patch=%+v err=%v", v, err)
	}
	if _, err = s.PatchSettings(ctx, 0, nil); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"password": "\"secret\"", "quality": "\"invalid\"", "autoplay": "null"} {
		if _, err = s.PatchSettings(ctx, 1, map[string]json.RawMessage{key: json.RawMessage(value)}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s accepted: %v", key, err)
		}
	}
	v, err = s.ResetSettings(ctx, 1)
	if err != nil || v.Autoplay || v.Revision != 2 {
		t.Fatalf("reset=%+v err=%v", v, err)
	}
}
func TestConcurrentRevisionHasOneWinner(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.PutProgress(ctx, "ep", ProgressUpdate{Mode: "sub"})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflict=%d", successes, conflicts)
	}
}
func TestForeignKeysAndEpisodeBatchRollback(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	if err := s.PutBookmark(ctx, "absent"); err == nil {
		t.Fatal("missing anime accepted")
	}
	err := s.UpsertEpisodes(ctx, []domain.Episode{{ID: "second", AnimeID: "anime", Label: "2", Ordinal: 2}, {ID: "invalid", AnimeID: "missing", Label: "3", Ordinal: 3}})
	if err == nil {
		t.Fatal("missing parent accepted")
	}
	items, err := s.Episodes(ctx, "anime")
	if err != nil || len(items) != 1 {
		t.Fatalf("partial transaction committed: %+v %v", items, err)
	}
}
func TestMigrationChecksumsAndFutureSchemaRejected(t *testing.T) {
	future := fmt.Sprintf("INSERT INTO schema_migrations VALUES(%d,'future','now')", len(migrations)+1)
	for _, query := range []string{"UPDATE schema_migrations SET checksum='tampered'", future} {
		t.Run(query, func(t *testing.T) {
			s, path := fixture(t)
			if _, err := s.db.Exec(query); err != nil {
				t.Fatal(err)
			}
			s.Close()
			other, err := Open(path)
			if err == nil {
				other.Close()
				t.Fatal("unsupported schema opened")
			}
		})
	}
}
func TestInvalidDurationCancellationAndSecrets(t *testing.T) {
	s, _ := fixture(t)
	ctx := context.Background()
	zero := 0.0
	p, err := s.PutProgress(ctx, "ep", ProgressUpdate{Mode: "sub", DurationSeconds: &zero})
	if err != nil || p.Completed || p.DurationSeconds != nil {
		t.Fatalf("zero duration must stay unknown: %+v %v", p, err)
	}
	negative := -1.0
	if _, err = s.PutProgress(ctx, "ep", ProgressUpdate{Mode: "sub", DurationSeconds: &negative, Revision: 1}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Settings(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	for _, payload := range []string{`{"cookie":"private"}`, `{"url":"https://example.com/file?token=private"}`} {
		if err := s.PutCache(ctx, "private", CacheEntry{Payload: json.RawMessage(payload), ExpiresAt: time.Now().Add(time.Hour), SchemaVersion: 1}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("secret accepted: %v", err)
		}
	}
}
func TestManualWatchedFlagsAndPrecedence(t *testing.T) {
	s, path := fixture(t)
	ctx := context.Background()
	if err := s.UpsertEpisodes(ctx, []domain.Episode{{ID: "ep2", AnimeID: "anime", Label: "OVA-2", Ordinal: 2}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertAnime(ctx, domain.Anime{ID: "other", Title: "Other"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEpisodes(ctx, []domain.Episode{{ID: "ep-other", AnimeID: "other", Label: "1", Ordinal: 1}}); err != nil {
		t.Fatal(err)
	}
	// Mark externally watched without any progress: no invented duration or
	// position, and a synthesized row lets the player start at zero.
	p, err := s.SetWatched(ctx, "ep", true)
	if err != nil || !p.Completed || p.DurationSeconds != nil || p.PositionSeconds != 0 {
		t.Fatalf("watched: %+v %v", p, err)
	}
	// Idempotent set.
	if _, err = s.SetWatched(ctx, "ep", true); err != nil {
		t.Fatal(err)
	}
	// Undo with no automatic completion: consistent unwatch.
	p, err = s.SetWatched(ctx, "ep", false)
	if err != nil || p.Completed {
		t.Fatalf("unwatch: %+v %v", p, err)
	}
	// Automatic completion exists; explicit unwatch overrides it.
	duration := 100.0
	if _, err = s.PutProgress(ctx, "ep", ProgressUpdate{PositionSeconds: 96, DurationSeconds: &duration, Mode: "sub", Provider: "hianime"}); err != nil {
		t.Fatal(err)
	}
	p, err = s.SetWatched(ctx, "ep", false)
	if err != nil || p.Completed || p.PositionSeconds != 96 {
		t.Fatalf("unwatch must keep position and override completion: %+v %v", p, err)
	}
	// Manual watched wins over an automatic completion too.
	if _, err = s.PutProgress(ctx, "ep2", ProgressUpdate{PositionSeconds: 96, DurationSeconds: &duration, Mode: "sub", Provider: "hianime"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetWatched(ctx, "ep2", false); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetWatched(ctx, "ep2", true); err != nil {
		t.Fatal(err)
	}
	states, err := s.WatchedStates(ctx, "anime")
	// ep: automatic completion overridden by explicit unwatch; ep2: manual watched.
	if err != nil || len(states) != 2 || states[0].Watched || !states[1].Watched {
		t.Fatalf("states: %+v %v", states, err)
	}
	// Bulk actions only affect the selected title and use the real inventory.
	n, err := s.SetWatchedBulk(ctx, "other", true)
	if err != nil || n != 1 {
		t.Fatalf("bulk: %d %v", n, err)
	}
	states, err = s.WatchedStates(ctx, "anime")
	if err != nil || states[0].Watched || !states[1].Watched {
		t.Fatalf("bulk leaked across titles: %+v %v", states, err)
	}
	// Bulk unwatch undo is consistent.
	if _, err = s.SetWatchedBulk(ctx, "other", false); err != nil {
		t.Fatal(err)
	}
	if states, err = s.WatchedStates(ctx, "other"); err != nil || states[0].Watched {
		t.Fatalf("bulk unwatch: %+v %v", states, err)
	}
	// Invalid inputs fail closed.
	if _, err = s.SetWatched(ctx, "", true); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err = s.SetWatched(ctx, "missing", true); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = s.SetWatchedBulk(ctx, "", true); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	// Automatic completion takes the flag back from a manual unwatch...
	if _, err = s.PutProgress(ctx, "ep", ProgressUpdate{PositionSeconds: 96, DurationSeconds: &duration, Mode: "sub", Provider: "hianime", Revision: 1}); err != nil {
		t.Fatal(err)
	}
	if p, err = s.Progress(ctx, "ep"); err != nil || !p.Completed {
		t.Fatalf("automatic completion lost: %+v %v", p, err)
	}
	// ...and a progress reset also clears the manual assertion.
	if _, err = s.ResetProgress(ctx, "ep2", 1); err != nil {
		t.Fatal(err)
	}
	// Persist across restart: history and resume agree.
	s.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if p, err = reopened.Progress(ctx, "ep"); err != nil || !p.Completed || p.PositionSeconds != 96 {
		t.Fatalf("restart progress: %+v %v", p, err)
	}
	page, err := reopened.History(ctx, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if item.Episode.ID == "ep" && item.Progress.Completed != p.Completed {
			t.Fatalf("history and progress disagree: %+v", item)
		}
	}
	if states, err = reopened.WatchedStates(ctx, "other"); err != nil || states[0].Watched {
		t.Fatalf("restart states: %+v %v", states, err)
	}
}
