package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"myanime/internal/domain"
	"myanime/internal/provider"
)

// stubCatalog is a deterministic catalogue service: it never touches the
// network and its behavior is switched per test.
type stubCatalog struct {
	fail       error
	search     domain.CatalogPage
	searches   int
	schedule   any
	schedules  int
	tags       any
	tagFetches int
}

func (s *stubCatalog) Search(ctx context.Context, q domain.CatalogQuery) (domain.CatalogPage, error) {
	s.searches++
	if s.fail != nil {
		return domain.CatalogPage{Items: []domain.Anime{}}, s.fail
	}
	return s.search, nil
}
func (s *stubCatalog) Schedule(ctx context.Context) (any, error) {
	s.schedules++
	if s.fail != nil {
		return nil, s.fail
	}
	return s.schedule, nil
}
func (s *stubCatalog) Tags(ctx context.Context) (any, error) {
	s.tagFetches++
	if s.fail != nil {
		return nil, s.fail
	}
	return s.tags, nil
}

func (s *stubCatalog) Detail(context.Context, string) (domain.Anime, error) {
	return domain.Anime{}, errors.New("unused")
}
func (s *stubCatalog) Episodes(context.Context, string) ([]domain.Episode, error) {
	return nil, errors.New("unused")
}
func (s *stubCatalog) Resolve(context.Context, domain.ResolveRequest) (domain.Source, error) {
	return domain.Source{}, errors.New("unused")
}
func (s *stubCatalog) Providers() []domain.ProviderInfo { return nil }
func (s *stubCatalog) Close()                           {}
func (s *stubCatalog) Refresh()                         {}
func (s *stubCatalog) CheckAllHealth(context.Context)   {}
func (s *stubCatalog) Genres(context.Context) (any, error) {
	return nil, errors.New("unused")
}
func (s *stubCatalog) MangaSearch(context.Context, string, string, int) ([]domain.Manga, string, error) {
	return nil, "", errors.New("unused")
}
func (s *stubCatalog) MangaDetail(context.Context, string) (domain.Manga, error) {
	return domain.Manga{}, errors.New("unused")
}
func (s *stubCatalog) MangaChapters(context.Context, string) ([]domain.Chapter, error) {
	return nil, errors.New("unused")
}
func (s *stubCatalog) MangaChapter(context.Context, string) (domain.Chapter, error) {
	return domain.Chapter{}, errors.New("unused")
}
func (s *stubCatalog) MangaPages(context.Context, string) ([]domain.MangaPage, error) {
	return nil, errors.New("unused")
}

type offlineClient struct {
	t       *testing.T
	app     *App
	server  *httptest.Server
	session string
}

func newOfflineClient(t *testing.T, stub catalogService) *offlineClient {
	t.Helper()
	a, err := New(Config{Mode: "desktop", DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	a.catalog = stub
	credentials := a.Bootstrap()
	server := httptest.NewServer(a.Handler())
	t.Cleanup(func() {
		server.Close()
		if err := a.Close(); err != nil {
			t.Error(err)
		}
	})
	return &offlineClient{t: t, app: a, server: server, session: credentials["apiToken"]}
}

func (c *offlineClient) get(path string) (int, map[string]json.RawMessage) {
	c.t.Helper()
	req, err := http.NewRequest(http.MethodGet, c.server.URL+path, nil)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+c.session)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var envelope struct {
		Data  map[string]json.RawMessage `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		c.t.Fatalf("decode response: %v", err)
	}
	if resp.StatusCode != 200 {
		if envelope.Error == nil {
			c.t.Fatalf("status %d without error body", resp.StatusCode)
		}
		return resp.StatusCode, map[string]json.RawMessage{"error": json.RawMessage(envelope.Error.Code)}
	}
	return resp.StatusCode, envelope.Data
}

func popularPage() domain.CatalogPage {
	return domain.CatalogPage{Items: []domain.Anime{{
		ID: "anilist:1", Title: "Cached Title", PosterURL: "https://img.example/cover.jpg",
		Availability: []string{"sub"},
	}}}
}

// Warm cache + upstream failure serves the cached page labelled stale.
func TestCatalogStaleFallbackServesCachedItems(t *testing.T) {
	stub := &stubCatalog{search: popularPage()}
	c := newOfflineClient(t, stub)

	status, data := c.get("/api/v1/catalog?limit=24")
	if status != 200 || string(data["stale"]) != "" {
		t.Fatalf("fresh fetch: status=%d stale=%s", status, data["stale"])
	}
	stub.fail = providerFailure("unavailable")

	status, data = c.get("/api/v1/catalog?limit=24")
	if status != 200 {
		t.Fatalf("stale fetch: status=%d", status)
	}
	var stale struct {
		Items []domain.Anime `json:"items"`
		Stale bool           `json:"stale"`
	}
	if err := json.Unmarshal(data["items"], &stale.Items); err != nil {
		t.Fatal(err)
	}
	json.Unmarshal(data["stale"], &stale.Stale)
	if !stale.Stale || len(stale.Items) != 1 || stale.Items[0].ID != "anilist:1" {
		t.Fatalf("unexpected stale payload: %+v", stale)
	}
	// Playback availability is never sourced from the catalogue cache.
	if len(stale.Items[0].Availability) != 0 {
		t.Fatalf("cached item advertised availability %v", stale.Items[0].Availability)
	}
}

// Empty cache + upstream failure reports an honest offline error.
func TestCatalogOfflineWithoutCache(t *testing.T) {
	stub := &stubCatalog{fail: providerFailure("timeout")}
	c := newOfflineClient(t, stub)

	status, data := c.get("/api/v1/catalog?limit=24")
	if status != 503 || string(data["error"]) != "CATALOG_OFFLINE" {
		t.Fatalf("expected 503 CATALOG_OFFLINE, got %d %s", status, data["error"])
	}
}

// Recovery: a successful fetch replaces the stale payload and clears the flag.
func TestCatalogRecoveryReplacesStaleData(t *testing.T) {
	stub := &stubCatalog{search: popularPage()}
	c := newOfflineClient(t, stub)
	c.get("/api/v1/catalog?limit=24")

	stub.fail = providerFailure("unavailable")
	if status, _ := c.get("/api/v1/catalog?limit=24"); status != 200 {
		t.Fatal("stale fetch failed")
	}
	stub.fail = nil
	stub.search = domain.CatalogPage{Items: []domain.Anime{{ID: "anilist:2", Title: "Fresh Title"}}}

	status, data := c.get("/api/v1/catalog?limit=24")
	if status != 200 || string(data["stale"]) == "true" {
		t.Fatalf("recovered fetch should be fresh: status=%d stale=%s", status, data["stale"])
	}
	var items []domain.Anime
	if err := json.Unmarshal(data["items"], &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "anilist:2" {
		t.Fatalf("recovered fetch did not replace stale data: %+v", items)
	}
	// And the next outage serves the replaced payload.
	stub.fail = providerFailure("rate_limited")
	_, data = c.get("/api/v1/catalog?limit=24")
	json.Unmarshal(data["items"], &items)
	if len(items) != 1 || items[0].ID != "anilist:2" {
		t.Fatalf("post-recovery stale fetch served old data: %+v", items)
	}
}

// Non-transient errors never fall back to the cache.
func TestCatalogNonTransientSkipsCache(t *testing.T) {
	stub := &stubCatalog{search: popularPage()}
	c := newOfflineClient(t, stub)
	if status, _ := c.get("/api/v1/catalog?limit=24"); status != 200 {
		t.Fatal("warm-up fetch failed")
	}
	stub.fail = providerFailure("invalid_season")

	status, data := c.get("/api/v1/catalog?limit=24")
	if status == 200 {
		t.Fatalf("invalid input must not serve cache (stale=%s)", data["stale"])
	}
}

// Schedule metadata falls back with the stale label too.
func TestScheduleStaleFallback(t *testing.T) {
	stub := &stubCatalog{schedule: map[string]any{"items": []map[string]any{{
		"id": 1, "airingAt": int64(1700000000), "episode": 1,
		"anime": domain.Anime{ID: "anilist:1", Title: "Airing", Availability: []string{"sub"}},
	}}}}
	c := newOfflineClient(t, stub)
	if status, _ := c.get("/api/v1/schedule"); status != 200 {
		t.Fatal("warm-up fetch failed")
	}
	stub.fail = providerFailure("unavailable")

	status, data := c.get("/api/v1/schedule")
	if status != 200 || string(data["stale"]) != "true" {
		t.Fatalf("schedule stale fetch: status=%d stale=%s", status, data["stale"])
	}
	var payload struct {
		Items []struct {
			Anime domain.Anime `json:"anime"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data["items"], &payload.Items); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 1 || len(payload.Items[0].Anime.Availability) != 0 {
		t.Fatalf("cached schedule advertised availability: %+v", payload.Items)
	}
}

// splitList flattens the two accepted list spellings: repeated parameters and
// comma-separated values, dropping blanks either way.
func TestSplitList(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []string
		want   []string
	}{
		{"absent", nil, nil},
		{"single", []string{"Action"}, []string{"Action"}},
		{"comma", []string{"Action,Comedy"}, []string{"Action", "Comedy"}},
		{"repeated", []string{"Action", "Comedy"}, []string{"Action", "Comedy"}},
		{"mixed", []string{"Action, Comedy", "Drama"}, []string{"Action", "Comedy", "Drama"}},
		{"blanks", []string{"", "  ", " ,Action,,Comedy, "}, []string{"Action", "Comedy"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := splitList(tc.values); !slices.Equal(got, tc.want) {
				t.Fatalf("splitList(%q) = %q, want %q", tc.values, got, tc.want)
			}
		})
	}
}

// catalogueKey hashes every filter field, so a stale page cached for one
// filter set can never answer another. Genre/tag order stays significant:
// the key is built from the parsed request unchanged, and order-insensitive
// keys are not needed because the provider normalizes the upstream query.
func TestCatalogueKeyCoversEveryFilter(t *testing.T) {
	base := domain.CatalogQuery{
		Query: "q", Cursor: "2", Season: "winter", Status: "releasing",
		Type: "tv", Sort: "score", Genres: []string{"Action"},
		Tags: []string{"Isekai"}, Year: 2024, Limit: 24,
	}
	key := catalogueKey("catalog", base)
	same := base
	same.Genres = []string{"Action"}
	same.Tags = []string{"Isekai"}
	if catalogueKey("catalog", same) != key {
		t.Fatal("identical filters produced different keys")
	}
	if catalogueKey("schedule", base) == key {
		t.Fatal("kind is not part of the key")
	}
	for _, tc := range []struct {
		field  string
		mutate func(*domain.CatalogQuery)
	}{
		{"query", func(q *domain.CatalogQuery) { q.Query = "other" }},
		{"cursor", func(q *domain.CatalogQuery) { q.Cursor = "3" }},
		{"season", func(q *domain.CatalogQuery) { q.Season = "spring" }},
		{"status", func(q *domain.CatalogQuery) { q.Status = "finished" }},
		{"type", func(q *domain.CatalogQuery) { q.Type = "movie" }},
		{"sort", func(q *domain.CatalogQuery) { q.Sort = "newest" }},
		{"genres", func(q *domain.CatalogQuery) { q.Genres = []string{"Comedy"} }},
		{"tags", func(q *domain.CatalogQuery) { q.Tags = []string{"Historical"} }},
		{"year", func(q *domain.CatalogQuery) { q.Year = 2023 }},
		{"limit", func(q *domain.CatalogQuery) { q.Limit = 12 }},
	} {
		t.Run(tc.field, func(t *testing.T) {
			changed := base
			tc.mutate(&changed)
			if catalogueKey("catalog", changed) == key {
				t.Fatalf("%s change left the cache key unchanged", tc.field)
			}
		})
	}
	// Field boundaries are unambiguous: a comma inside one term is a
	// different filter set than two terms.
	one := domain.CatalogQuery{Genres: []string{"Action,Comedy"}}
	two := domain.CatalogQuery{Genres: []string{"Action", "Comedy"}}
	if catalogueKey("catalog", one) == catalogueKey("catalog", two) {
		t.Fatal("list separator collides with a literal comma")
	}
}

// Tag metadata falls back through the shared facet cache path too.
func TestTagsStaleFallback(t *testing.T) {
	stub := &stubCatalog{tags: map[string]any{"items": []map[string]string{
		{"id": "Isekai", "name": "Isekai", "category": "Theme"},
	}}}
	c := newOfflineClient(t, stub)
	if status, _ := c.get("/api/v1/tags"); status != 200 {
		t.Fatal("warm-up fetch failed")
	}
	stub.fail = providerFailure("unavailable")

	status, data := c.get("/api/v1/tags")
	if status != 200 || string(data["stale"]) != "true" {
		t.Fatalf("tags stale fetch: status=%d stale=%s", status, data["stale"])
	}
	var items []map[string]string
	if err := json.Unmarshal(data["items"], &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0]["category"] != "Theme" {
		t.Fatalf("cached tags lost their category: %+v", items)
	}
}

func providerFailure(code string) error {
	return &provider.Error{Code: code, Provider: "anilist"}
}
