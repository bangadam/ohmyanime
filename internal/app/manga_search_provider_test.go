package app

import (
	"context"
	"encoding/json"
	"testing"

	"myanime/internal/domain"
)

// scopedCatalog records which search path a route took so the test can prove a
// provider filter never degrades into an unfiltered search.
type scopedCatalog struct {
	stubCatalog
	scopedCalls   int
	scopedArgs    []string
	scopedItems   []domain.Manga
	unfilteredHit int
}

func (s *scopedCatalog) MangaSearchProvider(_ context.Context, provider, query, cursor string, limit int) ([]domain.Manga, string, error) {
	s.scopedCalls++
	s.scopedArgs = []string{provider, query, cursor}
	return s.scopedItems, "", nil
}

func (s *scopedCatalog) MangaSearch(context.Context, string, string, int) ([]domain.Manga, string, error) {
	s.unfilteredHit++
	return []domain.Manga{{ID: "mangadex:unfiltered", Title: "Unfiltered"}}, "", nil
}

func TestHTTPMangaSearchProviderFilter(t *testing.T) {
	stub := &scopedCatalog{scopedItems: []domain.Manga{{ID: "mangafire:abc", Title: "Filtered"}}}
	c := newOfflineClient(t, stub)

	code, data := c.get("/api/v1/manga?query=one&provider=mangafire")
	if code != 200 {
		t.Fatalf("code %d", code)
	}
	if stub.scopedCalls != 1 || stub.unfilteredHit != 0 {
		t.Fatalf("scoped=%d unfiltered=%d, want the scoped path only", stub.scopedCalls, stub.unfilteredHit)
	}
	if stub.scopedArgs[0] != "mangafire" || stub.scopedArgs[1] != "one" {
		t.Fatalf("provider/query forwarded as %v", stub.scopedArgs)
	}
	var items []domain.Manga
	if err := json.Unmarshal(data["items"], &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "mangafire:abc" {
		t.Fatalf("items = %+v", items)
	}

	// Without the filter the merged path still runs.
	if code, _ := c.get("/api/v1/manga?query=one"); code != 200 {
		t.Fatalf("unfiltered code %d", code)
	}
	if stub.unfilteredHit != 1 {
		t.Fatalf("unfiltered calls = %d, want 1", stub.unfilteredHit)
	}
}

func TestHTTPMangaSearchProviderFilterRequiresSupport(t *testing.T) {
	// A catalogue that cannot scope a search must fail loudly rather than
	// return unrelated providers' results for an explicit selection.
	c := newOfflineClient(t, &stubCatalog{})
	code, _ := c.get("/api/v1/manga?query=one&provider=mangafire")
	if code == 200 {
		t.Fatal("provider filter silently ignored")
	}
}
