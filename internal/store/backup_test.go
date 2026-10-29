package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"myanime/internal/domain"
)

func library(t *testing.T) (*Store, string) {
	t.Helper()
	s, path := fixture(t)
	ctx := context.Background()
	if err := s.PutBookmark(ctx, "anime"); err != nil {
		t.Fatal(err)
	}
	duration := 200.0
	if _, err := s.PutProgress(ctx, "ep", ProgressUpdate{PositionSeconds: 42, DurationSeconds: &duration, Mode: "sub", Provider: "hianime"}); err != nil {
		t.Fatal(err)
	}
	return s, path
}

func TestExportImportRoundTrip(t *testing.T) {
	s, _ := library(t)
	ctx := context.Background()
	dir := t.TempDir()
	export := filepath.Join(dir, "export.sqlite")
	if err := s.Export(ctx, export); err != nil {
		t.Fatal(err)
	}
	if err := s.Export(ctx, export); err != nil {
		t.Fatalf("re-export must replace the previous file: %v", err)
	}

	restoredPath := filepath.Join(t.TempDir(), "restored.sqlite")
	restored, err := Open(restoredPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if _, err := restored.Progress(ctx, "ep"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("fresh library must be empty before import: %v", err)
	}
	if err = restored.Import(ctx, export); err != nil {
		t.Fatal(err)
	}
	page, err := restored.Bookmarks(ctx, 20, "")
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "anime" {
		t.Fatalf("bookmarks after restore: %+v err=%v", page, err)
	}
	p, err := restored.Progress(ctx, "ep")
	if err != nil || p.PositionSeconds != 42 || p.Revision != 1 || p.Mode != "sub" || p.Completed {
		t.Fatalf("progress after restore: %+v err=%v", p, err)
	}
	if _, err = os.Stat(restoredPath + ".pre-restore"); err != nil {
		t.Fatalf("pre-restore backup missing: %v", err)
	}
}

func TestImportRejectsIncompatibleAndDamaged(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(t *testing.T) string{
		"garbage bytes": func(t *testing.T) string {
			p := filepath.Join(t.TempDir(), "garbage.sqlite")
			if err := os.WriteFile(p, []byte("this is definitely not a sqlite database"), 0600); err != nil {
				t.Fatal(err)
			}
			return p
		},
		"tampered migration checksum": func(t *testing.T) string {
			s, _ := library(t)
			if _, err := s.db.Exec("UPDATE schema_migrations SET checksum='tampered' WHERE version=1"); err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(t.TempDir(), "tampered.sqlite")
			if err := s.Export(ctx, p); err != nil {
				t.Fatal(err)
			}
			return p
		},
		"future schema version": func(t *testing.T) string {
			s, _ := library(t)
			if _, err := s.db.Exec("INSERT INTO schema_migrations VALUES(99,'future','now')"); err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(t.TempDir(), "future.sqlite")
			if err := s.Export(ctx, p); err != nil {
				t.Fatal(err)
			}
			return p
		},
		"foreign sqlite schema": func(t *testing.T) string {
			s, _ := library(t)
			if _, err := s.db.Exec("DROP TABLE schema_migrations"); err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(t.TempDir(), "foreign.sqlite")
			if err := s.Export(ctx, p); err != nil {
				t.Fatal(err)
			}
			return p
		},
	}
	for name, make := range cases {
		t.Run(name, func(t *testing.T) {
			bad := make(t)
			s, path := library(t)
			if err := s.Import(ctx, bad); !errors.Is(err, ErrIncompatibleImport) {
				t.Fatalf("import must be rejected: %v", err)
			}
			// Original data must remain usable, both live and after reopen.
			assertLibraryIntact(t, s, path, "after rejected import")
		})
	}
}

func assertLibraryIntact(t *testing.T, s *Store, path, when string) {
	t.Helper()
	ctx := context.Background()
	page, err := s.Bookmarks(ctx, 20, "")
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("bookmarks lost %s: %+v err=%v", when, page, err)
	}
	p, err := s.Progress(ctx, "ep")
	if err != nil || p.PositionSeconds != 42 {
		t.Fatalf("progress lost %s: %+v err=%v", when, p, err)
	}
	if err = s.Close(); err != nil {
		t.Fatalf("close %s: %v", when, err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen %s: %v", when, err)
	}
	defer s.Close()
	if p, err = s.Progress(ctx, "ep"); err != nil || p.PositionSeconds != 42 {
		t.Fatalf("progress lost after reopen %s: %+v err=%v", when, p, err)
	}
}

func TestImportRejectsLiveFileAndBadArguments(t *testing.T) {
	ctx := context.Background()
	s, path := library(t)
	for name, target := range map[string]string{
		"live library": path,
		"empty":        "",
		"directory":    filepath.Dir(path),
	} {
		if err := s.Import(ctx, target); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s must be rejected with ErrInvalid: %v", name, err)
		}
	}
	assertLibraryIntact(t, s, path, "after bad import arguments")
}

func TestExportToleratesConcurrentWrites(t *testing.T) {
	s, _ := library(t)
	ctx := context.Background()
	duration := 200.0
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		rev := int64(1)
		pos := 43.0
		for {
			select {
			case <-stop:
				return
			default:
			}
			p, err := s.PutProgress(ctx, "ep", ProgressUpdate{PositionSeconds: pos, DurationSeconds: &duration, Mode: "sub", Revision: rev, Provider: "hianime"})
			if err != nil {
				t.Errorf("concurrent write: %v", err)
				return
			}
			rev = p.Revision
			pos++
		}
	}()
	dir := t.TempDir()
	var exports []string
	for i := range 5 {
		p := filepath.Join(dir, fmt.Sprintf("e%d.sqlite", i))
		if err := s.Export(ctx, p); err != nil {
			t.Errorf("export under load: %v", err)
			continue
		}
		exports = append(exports, p)
	}
	close(stop)
	wg.Wait()
	if len(exports) != 5 {
		t.Fatalf("expected 5 exports, got %d", len(exports))
	}
	for _, p := range exports {
		if err := verifyLibrary(p); err != nil {
			t.Fatalf("export %s is damaged: %v", p, err)
		}
		db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: p, RawQuery: "mode=ro"}).String())
		if err != nil {
			t.Fatal(err)
		}
		var n int
		var revision int64
		var position float64
		err = db.QueryRow("SELECT COUNT(*),MAX(revision),MAX(position) FROM watch_progress").Scan(&n, &revision, &position)
		db.Close()
		if err != nil || n != 1 || revision < 1 || position < 1 {
			t.Fatalf("export %s is not a consistent snapshot: n=%d rev=%d pos=%f err=%v", p, n, revision, position, err)
		}
	}
}

var _ = domain.Anime{} // keep the domain import aligned with the test package conventions
