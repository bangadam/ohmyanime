package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"myanime/internal/domain"
	"myanime/internal/logging"
	"myanime/internal/provider"
	"myanime/internal/store"
)

// Offline catalogue fallback (issue 5.3). Transient upstream failures serve
// the last-known validated catalogue metadata from metadata_cache, labelled
// stale, so the browse pages stay usable while the source is unreachable.
// The cache holds public metadata only and playback availability is stripped
// before serving: a cached title is never advertised as an offline download
// or playable stream. An empty cache fails honestly instead of inventing
// data, and any later successful fetch replaces the stale payload.

const (
	catalogueCacheSchema = 2
	catalogueCacheTTL    = 24 * time.Hour
)

// catalogService is the slice of the provider service the app layer uses.
// An interface keeps the offline fallback deterministically testable.
type catalogService interface {
	domain.Catalog
	Close()
	Refresh()
	CheckAllHealth(ctx context.Context)
	MangaSearch(ctx context.Context, query, cursor string, limit int) ([]domain.Manga, string, error)
	MangaDetail(ctx context.Context, id string) (domain.Manga, error)
	MangaChapters(ctx context.Context, mangaID string) ([]domain.Chapter, error)
	MangaChapter(ctx context.Context, chapterID string) (domain.Chapter, error)
	MangaPages(ctx context.Context, chapterID string) ([]domain.MangaPage, error)
}

// mangaProviderSearch is the optional capability behind an explicit provider
// filter. Test doubles stay free to implement only the plain catalogue surface.
type mangaProviderSearch interface {
	MangaSearchProvider(ctx context.Context, provider, query, cursor string, limit int) ([]domain.Manga, string, error)
}

// transientUpstream reports whether an upstream failure is a connectivity or
// availability problem worth masking with last-known catalogue data. Input
// and mapping errors are not: caching cannot make them succeed.
func transientUpstream(err error) bool {
	var pe *provider.Error
	return errors.As(err, &pe) && (pe.Code == "timeout" || pe.Code == "unavailable" || pe.Code == "rate_limited")
}

// catalogueKey names one cached page. Every filter field is hashed: a stale
// page must never answer a different filter set, so a new CatalogQuery field
// must be added here as well.
// Genre and tag order is significant (no sorting): the cache is keyed by the
// request as parsed, which keeps the hash cheap and the mapping obvious.
func catalogueKey(kind string, q domain.CatalogQuery) string {
	h := sha256.Sum256([]byte(strings.Join([]string{
		kind,
		q.Query,
		q.Cursor,
		q.Season,
		q.Status,
		q.Type,
		q.Sort,
		strings.Join(q.Genres, "\x1f"),
		strings.Join(q.Tags, "\x1f"),
		strconv.Itoa(q.Year),
		strconv.Itoa(q.Limit),
	}, "\x00")))
	return "app:" + kind + ":" + hex.EncodeToString(h[:])
}

// cacheCatalogue persists a successful catalogue page for offline reuse.
// Failures are logged and never mask the fresh result.
func (a *App) cacheCatalogue(ctx context.Context, key string, payload any, logs *logging.Logger) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if err := a.db.PutCache(ctx, key, store.CacheEntry{Payload: data, ExpiresAt: time.Now().Add(catalogueCacheTTL), SchemaVersion: catalogueCacheSchema}); err != nil && logs != nil {
		logs.Error("catalogue_cache_write_failed", "error", err)
	}
}

// serveStaleCatalogue replies with the cached page labelled stale, or false
// when no validated cache exists. Availability is stripped so cached titles
// never advertise playable streams.
func (a *App) serveStaleCatalogue(ctx context.Context, w http.ResponseWriter, key string) bool {
	entry, err := a.db.StaleCache(ctx, key, catalogueCacheSchema)
	if err != nil {
		return false
	}
	var page domain.CatalogPage
	if json.Unmarshal(entry.Payload, &page) != nil {
		return false
	}
	items := make([]domain.Anime, 0, len(page.Items))
	for _, item := range page.Items {
		item.Availability = []string{}
		items = append(items, item)
	}
	reply(w, 200, map[string]any{"items": items, "nextCursor": page.NextCursor, "stale": true})
	return true
}

// scheduleCache mirrors the schedule endpoint's wire shape so stale payloads
// can be re-validated and sanitized before serving.
type scheduleCache struct {
	Items []struct {
		ID       int          `json:"id"`
		AiringAt int64        `json:"airingAt"`
		Episode  float64      `json:"episode"`
		Anime    domain.Anime `json:"anime"`
	} `json:"items"`
}

func (a *App) serveStaleSchedule(ctx context.Context, w http.ResponseWriter) bool {
	entry, err := a.db.StaleCache(ctx, "app:schedule", catalogueCacheSchema)
	if err != nil {
		return false
	}
	var payload scheduleCache
	if json.Unmarshal(entry.Payload, &payload) != nil {
		return false
	}
	for i := range payload.Items {
		payload.Items[i].Anime.Availability = []string{}
	}
	reply(w, 200, map[string]any{"items": payload.Items, "stale": true})
	return true
}

// serveStaleFacet replies with a cached facet vocabulary (genres, tags)
// labelled stale, or false when nothing validated is cached.
func (a *App) serveStaleFacet(ctx context.Context, w http.ResponseWriter, name string) bool {
	entry, err := a.db.StaleCache(ctx, "app:"+name, catalogueCacheSchema)
	if err != nil {
		return false
	}
	var payload struct {
		Items []map[string]string `json:"items"`
	}
	if json.Unmarshal(entry.Payload, &payload) != nil {
		return false
	}
	reply(w, 200, map[string]any{"items": payload.Items, "stale": true})
	return true
}

// offlineCatalogue is the honest empty-cache response: the source is
// unreachable and nothing cached exists to show.
func offlineCatalogue(w http.ResponseWriter) {
	failure(w, 503, "CATALOG_OFFLINE", "Metadata source unreachable and no cached results are available")
}
