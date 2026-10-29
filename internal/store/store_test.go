package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
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
	for _, query := range []string{"UPDATE schema_migrations SET checksum='tampered'", "INSERT INTO schema_migrations VALUES(2,'future','now')"} {
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
