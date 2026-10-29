package store

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"
)

// Cache records are exclusively public metadata. Playback resources, cookies and
// tokenized URLs belong to the playback service's bounded in-memory caches.
type CacheEntry struct {
	Payload       json.RawMessage
	ExpiresAt     time.Time
	SchemaVersion int
}

func safeMetadata(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for k, x := range v {
			key := strings.ToLower(k)
			if strings.Contains(key, "token") || strings.Contains(key, "cookie") || strings.Contains(key, "password") || strings.Contains(key, "secret") || strings.Contains(key, "authorization") {
				return false
			}
			if !safeMetadata(x) {
				return false
			}
		}
	case []any:
		for _, x := range v {
			if !safeMetadata(x) {
				return false
			}
		}
	case string:
		if strings.Contains(v, "://") {
			u, err := url.Parse(v)
			if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				return false
			}
		}
	}
	return true
}
func (s *Store) PutCache(ctx context.Context, key string, e CacheEntry) error {
	var data any
	if !validID(key) || e.SchemaVersion < 1 || e.ExpiresAt.IsZero() || len(e.Payload) > 8<<20 || json.Unmarshal(e.Payload, &data) != nil || !safeMetadata(data) {
		return ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO metadata_cache VALUES(?,?,?,?) ON CONFLICT(key) DO UPDATE SET payload=excluded.payload,expires_at=excluded.expires_at,schema_version=excluded.schema_version`, key, []byte(e.Payload), e.ExpiresAt.UTC().Format(time.RFC3339Nano), e.SchemaVersion)
	return err
}
func (s *Store) Cache(ctx context.Context, key string, schemaVersion int) (CacheEntry, error) {
	var e CacheEntry
	var expiry string
	err := s.db.QueryRowContext(ctx, "SELECT payload,expires_at,schema_version FROM metadata_cache WHERE key=?", key).Scan(&e.Payload, &expiry, &e.SchemaVersion)
	if err != nil {
		return e, normalizeError(err)
	}
	e.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiry)
	if err != nil {
		return CacheEntry{}, err
	}
	if e.SchemaVersion != schemaVersion || !time.Now().Before(e.ExpiresAt) {
		return CacheEntry{}, ErrNotFound
	}
	return e, nil
}

// ProviderMapping must represent a verified unambiguous match. Evidence records
// the title/season/type checks, never a provider credential or streaming URL.
type ProviderMapping struct {
	Provider, AnimeID, ProviderRef string
	Evidence                       json.RawMessage
	VerifiedAt                     time.Time
}

func (s *Store) PutProviderMapping(ctx context.Context, m ProviderMapping) error {
	var data map[string]any
	if !providerID(m.Provider) || m.Provider == "" || !validID(m.AnimeID) || !validID(m.ProviderRef) || strings.Contains(m.ProviderRef, "://") || m.VerifiedAt.IsZero() || json.Unmarshal(m.Evidence, &data) != nil || len(data) == 0 || !safeMetadata(data) {
		return ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO provider_mappings VALUES(?,?,?,?,?) ON CONFLICT(provider,anime_id) DO UPDATE SET provider_ref=excluded.provider_ref,evidence_json=excluded.evidence_json,verified_at=excluded.verified_at`, m.Provider, m.AnimeID, m.ProviderRef, string(m.Evidence), m.VerifiedAt.UTC().Format(time.RFC3339Nano))
	return err
}
func (s *Store) ProviderMapping(ctx context.Context, provider, animeID string) (ProviderMapping, error) {
	m := ProviderMapping{Provider: provider, AnimeID: animeID}
	var evidence, verified string
	err := s.db.QueryRowContext(ctx, "SELECT provider_ref,evidence_json,verified_at FROM provider_mappings WHERE provider=? AND anime_id=?", provider, animeID).Scan(&m.ProviderRef, &evidence, &verified)
	if err != nil {
		return m, normalizeError(err)
	}
	m.Evidence = json.RawMessage(evidence)
	m.VerifiedAt, err = time.Parse(time.RFC3339Nano, verified)
	return m, err
}
func (s *Store) PutEpisodeMapping(ctx context.Context, provider, episodeID, providerEpisodeRef string) error {
	if !providerID(provider) || provider == "" || !validID(episodeID) || !validID(providerEpisodeRef) || strings.Contains(providerEpisodeRef, "://") {
		return ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO episode_mappings VALUES(?,?,?) ON CONFLICT(provider,episode_id) DO UPDATE SET provider_episode_ref=excluded.provider_episode_ref`, provider, episodeID, providerEpisodeRef)
	return err
}
func (s *Store) EpisodeMapping(ctx context.Context, provider, episodeID string) (string, error) {
	var ref string
	err := s.db.QueryRowContext(ctx, "SELECT provider_episode_ref FROM episode_mappings WHERE provider=? AND episode_id=?", provider, episodeID).Scan(&ref)
	return ref, normalizeError(err)
}
