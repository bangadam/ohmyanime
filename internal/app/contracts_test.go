package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"myanime/internal/domain"
	"myanime/internal/store"
)

type contractClient struct {
	t       *testing.T
	app     *App
	server  *httptest.Server
	session string
	csrf    string
}
type contractEnvelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		Retryable bool   `json:"retryable"`
	} `json:"error"`
	RequestID string `json:"requestId"`
}

func newContractClient(t *testing.T) *contractClient {
	t.Helper()
	a, err := New(Config{Mode: "desktop", DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	credentials := a.Bootstrap()
	server := httptest.NewServer(a.Handler())
	t.Cleanup(func() {
		server.Close()
		if err := a.Close(); err != nil {
			t.Error(err)
		}
	})
	// Seed canonical metadata directly, so no test resolves or probes a provider.
	ctx := context.Background()
	for _, id := range []string{"anime-a", "anime-b"} {
		if err := a.db.UpsertAnime(ctx, domain.Anime{ID: id, Title: id, Availability: []string{"sub"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.db.UpsertEpisodes(ctx, []domain.Episode{{ID: "episode-a", AnimeID: "anime-a", Label: "12.5", Ordinal: 12.5, Availability: []string{"sub"}}, {ID: "episode-b", AnimeID: "anime-b", Label: "OVA-1", Ordinal: 1, Availability: []string{"sub"}}}); err != nil {
		t.Fatal(err)
	}
	return &contractClient{t: t, app: a, server: server, session: credentials["apiToken"], csrf: credentials["csrfToken"]}
}
func (c *contractClient) request(method, path, body string, status int) contractEnvelope {
	c.t.Helper()
	req, err := http.NewRequest(method, c.server.URL+"/api/v1"+path, strings.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+c.session)
	req.Header.Set("X-CSRF-Token", c.csrf)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.server.Client().Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	if res.StatusCode != status {
		c.t.Fatalf("%s %s: status %d want %d: %s", method, path, res.StatusCode, status, b)
	}
	var env contractEnvelope
	if status == 204 {
		if len(b) != 0 {
			c.t.Fatalf("204 contained body: %s", b)
		}
		return env
	}
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		c.t.Fatalf("not JSON: %s", b)
	}
	if err := json.Unmarshal(b, &env); err != nil {
		c.t.Fatal(err)
	}
	if env.RequestID == "" {
		c.t.Fatal("missing requestId")
	}
	if status >= 400 {
		if env.Error == nil || env.Error.Code == "" || env.Error.Message == "" {
			c.t.Fatalf("invalid error envelope: %s", b)
		}
		if len(env.Data) != 0 {
			c.t.Fatalf("error also has data: %s", b)
		}
	} else if env.Error != nil || len(env.Data) == 0 {
		c.t.Fatalf("invalid success envelope: %s", b)
	}
	return env
}
func contractData[T any](t *testing.T, e contractEnvelope) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(e.Data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func TestHTTPSettingsContract(t *testing.T) {
	c := newContractClient(t)
	initial := contractData[store.Settings](t, c.request("GET", "/settings", "", 200))
	if initial.Revision != 0 || initial.Quality != "auto" {
		t.Fatalf("defaults: %+v", initial)
	}
	updated := contractData[store.Settings](t, c.request("PATCH", "/settings", `{"revision":0,"quality":"720p","autoplay":true,"subtitleLanguage":"en"}`, 200))
	if updated.Revision != 1 || updated.Quality != "720p" || !updated.Autoplay {
		t.Fatalf("patch: %+v", updated)
	}
	c.request("PATCH", "/settings", `{"revision":0,"quality":"480p"}`, 409)
	for _, body := range []string{`{"revision":1,"secret":"x"}`, `{"revision":1,"quality":"4k"}`, `{"revision":1,"autoplay":null}`, `{"quality":"auto"}`, `{"revision":1} {}`, `{"revision":`} {
		c.request("PATCH", "/settings", body, 400)
	}
	saved := contractData[store.Settings](t, c.request("GET", "/settings", "", 200))
	if saved != updated {
		t.Fatalf("failed patch mutated state: %+v", saved)
	}
	reset := contractData[store.Settings](t, c.request("POST", "/settings/reset", `{"revision":1}`, 200))
	if reset.Revision != 2 || reset.Autoplay || reset.Quality != "auto" || reset.SubtitleLanguage != "id" {
		t.Fatalf("reset: %+v", reset)
	}
}
func TestHTTPProgressBookmarksHistoryContract(t *testing.T) {
	c := newContractClient(t)
	if got := string(c.request("GET", "/progress/episode-a", "", 200).Data); got != "null" {
		t.Fatalf("missing progress = %s", got)
	}
	for _, id := range []string{"anime-a", "anime-a", "anime-b"} {
		c.request("PUT", "/bookmarks/"+id, "", 200)
	}
	first := contractData[store.BookmarkPage](t, c.request("GET", "/bookmarks?limit=1", "", 200))
	if len(first.Items) != 1 || first.NextCursor == nil {
		t.Fatalf("first page: %+v", first)
	}
	second := contractData[store.BookmarkPage](t, c.request("GET", "/bookmarks?limit=1&cursor="+*first.NextCursor, "", 200))
	if len(second.Items) != 1 || second.NextCursor != nil || second.Items[0].ID == first.Items[0].ID {
		t.Fatalf("second page: %+v", second)
	}
	p := contractData[store.Progress](t, c.request("PUT", "/progress/episode-a", `{"positionSeconds":95,"durationSeconds":100,"mode":"sub","provider":"hianime","revision":0}`, 200))
	if p.Revision != 1 || !p.Completed || p.EpisodeID != "episode-a" {
		t.Fatalf("progress: %+v", p)
	}
	if _, err := time.Parse(time.RFC3339Nano, p.UpdatedAt); err != nil {
		t.Fatal(err)
	}
	c.request("PUT", "/progress/episode-a", `{"positionSeconds":3,"durationSeconds":null,"mode":"sub","revision":0}`, 409)
	for _, body := range []string{`{"positionSeconds":-1,"mode":"sub","revision":1}`, `{"positionSeconds":1,"durationSeconds":-1,"mode":"sub","revision":1}`, `{"positionSeconds":1,"mode":"invalid","revision":1}`, `{"positionSeconds":1,"mode":"sub","revision":1,"unknown":true}`, `{"positionSeconds":1e999,"mode":"sub","revision":1}`} {
		c.request("PUT", "/progress/episode-a", body, 400)
	}
	p = contractData[store.Progress](t, c.request("PUT", "/progress/episode-a", `{"positionSeconds":2,"durationSeconds":null,"mode":"sub","revision":1}`, 200))
	if p.Revision != 2 || !p.Completed || p.DurationSeconds != nil {
		t.Fatalf("sticky completion: %+v", p)
	}
	saved := contractData[store.Progress](t, c.request("GET", "/progress/episode-a", "", 200))
	if saved.Revision != 2 || saved.PositionSeconds != 2 {
		t.Fatalf("saved: %+v", saved)
	}
	history := contractData[store.HistoryPage](t, c.request("GET", "/history", "", 200))
	if len(history.Items) != 1 || history.Items[0].Episode.Label != "12.5" || history.Items[0].Progress.Revision != 2 || history.Items[0].Provider != "hianime" {
		t.Fatalf("history: %+v", history)
	}
	c.request("DELETE", "/history", "", 400)
	history = contractData[store.HistoryPage](t, c.request("GET", "/history", "", 200))
	if len(history.Items) != 1 {
		t.Fatal("unconfirmed delete changed history")
	}
	c.request("DELETE", "/history?confirmed=true", "", 204)
	history = contractData[store.HistoryPage](t, c.request("GET", "/history", "", 200))
	if len(history.Items) != 0 || history.NextCursor != nil {
		t.Fatalf("history not empty: %+v", history)
	}
	if string(c.request("GET", "/progress/episode-a", "", 200).Data) != "null" {
		t.Fatal("progress survived history deletion")
	}
	bookmarks := contractData[store.BookmarkPage](t, c.request("GET", "/bookmarks", "", 200))
	if len(bookmarks.Items) != 2 {
		t.Fatal("history deletion changed bookmarks")
	}
	c.request("DELETE", "/bookmarks/anime-a", "", 204)
	c.request("DELETE", "/bookmarks/anime-a", "", 204)
	bookmarks = contractData[store.BookmarkPage](t, c.request("GET", "/bookmarks", "", 200))
	if len(bookmarks.Items) != 1 || bookmarks.Items[0].ID != "anime-b" {
		t.Fatalf("bookmark delete: %+v", bookmarks)
	}
}
func TestHTTPWatchedContract(t *testing.T) {
	c := newContractClient(t)
	// Per-episode mark: watched without any progress row (no invented duration).
	p := contractData[store.Progress](t, c.request("PUT", "/watched/episode-a", "", 200))
	if !p.Completed || p.DurationSeconds != nil || p.PositionSeconds != 0 || p.EpisodeID != "episode-a" {
		t.Fatalf("watched: %+v", p)
	}
	// Idempotent.
	c.request("PUT", "/watched/episode-a", "", 200)
	// Unknown episode rejected.
	c.request("PUT", "/watched/missing", "", 404)
	c.request("DELETE", "/watched/missing", "", 404)
	// A bulk action for a title with no stored inventory is an idempotent
	// no-op (count 0): bulk never invents episodes for unknown titles.
	c.request("POST", "/watched/bulk", `{"animeId":"missing","watched":true}`, 200)
	c.request("POST", "/watched/bulk", `{"watched":true}`, 400)
	c.request("POST", "/watched/bulk", `{"animeId":"anime-b","watched":true,"extra":1}`, 400)
	c.request("GET", "/watched", "", 400)
	// Bulk watched only affects the selected title.
	bulk := contractData[struct {
		AnimeID string `json:"animeId"`
		Watched bool   `json:"watched"`
		Count   int    `json:"count"`
	}](t, c.request("POST", "/watched/bulk", `{"animeId":"anime-b","watched":true}`, 200))
	if bulk.Count != 1 || !bulk.Watched || bulk.AnimeID != "anime-b" {
		t.Fatalf("bulk: %+v", bulk)
	}
	states := contractData[struct {
		Episodes []store.WatchedState `json:"episodes"`
	}](t, c.request("GET", "/watched?animeId=anime-b", "", 200))
	if len(states.Episodes) != 1 || states.Episodes[0].EpisodeID != "episode-b" || !states.Episodes[0].Watched {
		t.Fatalf("bulk states: %+v", states)
	}
	// Other title untouched.
	states = contractData[struct {
		Episodes []store.WatchedState `json:"episodes"`
	}](t, c.request("GET", "/watched?animeId=anime-a", "", 200))
	if len(states.Episodes) != 1 || !states.Episodes[0].Watched {
		t.Fatalf("bulk leaked into anime-a: %+v", states)
	}
	// Unwatch (episode-a had a manual watch, not progress).
	p = contractData[store.Progress](t, c.request("DELETE", "/watched/episode-a", "", 200))
	if p.Completed {
		t.Fatalf("unwatch: %+v", p)
	}
	// Bulk unwatch is consistent undo.
	c.request("POST", "/watched/bulk", `{"animeId":"anime-b","watched":false}`, 200)
	states = contractData[struct {
		Episodes []store.WatchedState `json:"episodes"`
	}](t, c.request("GET", "/watched?animeId=anime-b", "", 200))
	if len(states.Episodes) != 1 || states.Episodes[0].Watched {
		t.Fatalf("bulk unwatch: %+v", states)
	}
}
func TestHTTPErrorEnvelopeContract(t *testing.T) {
	c := newContractClient(t)
	for _, path := range []string{"/bookmarks?cursor=bad", "/history?cursor=-1"} {
		env := c.request("GET", path, "", 400)
		if env.Error.Retryable {
			t.Fatal("invalid input marked retryable")
		}
	}
	env := c.request("GET", "/does-not-exist", "", 404)
	if env.Error.Retryable {
		t.Fatal("not found marked retryable")
	}
	req, err := http.NewRequest("GET", c.server.URL+"/api/v1/settings", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("unauthenticated status %d", res.StatusCode)
	}
	var denied contractEnvelope
	if err := json.NewDecoder(res.Body).Decode(&denied); err != nil {
		t.Fatal(err)
	}
	if denied.RequestID == "" || denied.Error == nil || denied.Error.Code != "UNAUTHORIZED" {
		t.Fatalf("unauthorized envelope: %+v", denied)
	}
}

func TestHTTPConfirmedCleanupContract(t *testing.T) {
	c := newContractClient(t)
	c.request("PUT", "/bookmarks/anime-a", "", 200)
	c.request("PUT", "/progress/episode-a", `{"positionSeconds":5,"mode":"sub","revision":0}`, 200)
	for _, path := range []string{"/bookmarks", "/cache"} {
		c.request("DELETE", path, "", 400)
	}
	c.request("DELETE", "/cache?confirmed=true", "", 204)
	bookmarks := contractData[store.BookmarkPage](t, c.request("GET", "/bookmarks", "", 200))
	if len(bookmarks.Items) != 1 {
		t.Fatal("cache cleanup deleted bookmarks")
	}
	c.request("DELETE", "/bookmarks?confirmed=true", "", 204)
	bookmarks = contractData[store.BookmarkPage](t, c.request("GET", "/bookmarks", "", 200))
	if len(bookmarks.Items) != 0 {
		t.Fatal("bookmarks not cleared")
	}
	history := contractData[store.HistoryPage](t, c.request("GET", "/history", "", 200))
	if len(history.Items) != 1 {
		t.Fatal("cleanup deleted history")
	}
	progress := contractData[store.Progress](t, c.request("GET", "/progress/episode-a", "", 200))
	if progress.PositionSeconds != 5 {
		t.Fatal("cleanup deleted progress")
	}
	for _, path := range []string{"/bookmarks?limit=0", "/history?limit=51", "/bookmarks?limit=bad"} {
		c.request("GET", path, "", 400)
	}
}

func TestHTTPProvidersHealthContract(t *testing.T) {
	c := newContractClient(t)
	// Listing providers must surface health without probing anything:
	// the state starts unknown with no checkedAt.
	list := contractData[struct {
		Items []domain.ProviderInfo `json:"items"`
	}](t, c.request("GET", "/providers", "", 200))
	if len(list.Items) == 0 {
		t.Fatal("no providers listed")
	}
	for _, p := range list.Items {
		if p.Health.State != "unknown" {
			t.Fatalf("provider %s health = %q before any probe, want unknown", p.ID, p.Health.State)
		}
		if !p.Health.CheckedAt.IsZero() {
			t.Fatalf("provider %s unexpectedly probed at %v", p.ID, p.Health.CheckedAt)
		}
	}
	// Manual refresh probes real homepages, so it is a live smoke check:
	// skipped unless explicitly opted in, keeping the default suite offline.
	if os.Getenv("PROBE_SMOKE_NETWORK") == "" {
		t.Skip("set PROBE_SMOKE_NETWORK=1 to exercise a live health refresh")
	}
	// shape with a state and checkedAt for each entry, whatever the network
	// outcome is. Offline test machines still get a bounded result.
	refreshed := contractData[struct {
		Items []domain.ProviderInfo `json:"items"`
	}](t, c.request("POST", "/providers/health", "", 200))
	if len(refreshed.Items) != len(list.Items) {
		t.Fatalf("refresh returned %d items, want %d", len(refreshed.Items), len(list.Items))
	}
	for _, p := range refreshed.Items {
		if p.Health.State == "" || p.Health.State == "unknown" {
			t.Fatalf("provider %s health = %q after refresh, want a probed state", p.ID, p.Health.State)
		}
		if p.Health.CheckedAt.IsZero() {
			t.Fatalf("provider %s refresh result missing checkedAt", p.ID)
		}
		if !p.Enabled {
			t.Fatalf("provider %s disabled by health refresh", p.ID)
		}
	}
}

// Invalid catalogue filters are rejected by provider-side validation before
// any upstream request, so this contract test stays offline. They must answer
// 400 rather than masquerading as a provider outage.
func TestHTTPCatalogueInvalidFiltersContract(t *testing.T) {
	c := newContractClient(t)
	for _, path := range []string{
		"/catalog?sort=bogus",
		"/catalog?status=bogus",
		"/catalog?type=bogus",
		"/catalog?season=bogus",
		"/catalog?cursor=0",
		"/catalog?cursor=10001",
		"/catalog?year=bogus",
		"/catalog?year=0",
	} {
		env := c.request("GET", path, "", 400)
		if env.Error.Code != "INVALID_INPUT" {
			t.Fatalf("%s: code %q, want INVALID_INPUT", path, env.Error.Code)
		}
		if env.Error.Retryable {
			t.Fatalf("%s: invalid input marked retryable", path)
		}
	}
}
