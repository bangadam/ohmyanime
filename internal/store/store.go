// Package store owns durable, non-secret user data and metadata. Cache deletion,
// history deletion, bookmark deletion and settings reset are deliberately separate.
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
	"myanime/internal/domain"
)

var (
	ErrConflict = errors.New("state revision conflict")
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid stored data")
)

type Store struct{ db *sql.DB }

// The migration text is immutable: its SHA-256 is checked on every open.
// Future destructive migrations must first use a consistent SQLite backup, never
// a plain file copy of a live WAL database. Unknown versions fail closed.
const migration = `
CREATE TABLE anime (id TEXT PRIMARY KEY, title TEXT NOT NULL, metadata_json TEXT NOT NULL CHECK(json_valid(metadata_json)), fetched_at TEXT NOT NULL);
CREATE TABLE episodes (id TEXT PRIMARY KEY, anime_id TEXT NOT NULL REFERENCES anime(id), label TEXT NOT NULL, ordinal REAL NOT NULL, metadata_json TEXT NOT NULL CHECK(json_valid(metadata_json)), UNIQUE(anime_id,label));
CREATE TABLE provider_mappings (provider TEXT NOT NULL, anime_id TEXT NOT NULL REFERENCES anime(id), provider_ref TEXT NOT NULL, evidence_json TEXT NOT NULL CHECK(json_valid(evidence_json)), verified_at TEXT NOT NULL, PRIMARY KEY(provider,anime_id));
CREATE TABLE episode_mappings (provider TEXT NOT NULL, episode_id TEXT NOT NULL REFERENCES episodes(id), provider_episode_ref TEXT NOT NULL, PRIMARY KEY(provider,episode_id));
CREATE TABLE bookmarks (anime_id TEXT PRIMARY KEY REFERENCES anime(id), created_at TEXT NOT NULL);
CREATE TABLE watch_progress (episode_id TEXT PRIMARY KEY REFERENCES episodes(id), position REAL NOT NULL CHECK(position>=0), duration REAL CHECK(duration IS NULL OR duration>0), mode TEXT NOT NULL CHECK(mode IN ('sub','dub')), completed INTEGER NOT NULL CHECK(completed IN (0,1)), revision INTEGER NOT NULL CHECK(revision>0), updated_at TEXT NOT NULL);
CREATE TABLE watch_history (episode_id TEXT PRIMARY KEY REFERENCES episodes(id), last_watched_at TEXT NOT NULL, provider TEXT NOT NULL, mode TEXT NOT NULL CHECK(mode IN ('sub','dub')));
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL CHECK(json_valid(value)), revision INTEGER NOT NULL CHECK(revision>=0));
INSERT INTO settings VALUES ('preferences','{"subtitleLanguage":"id","quality":"auto","preferredProvider":"auto","autoplay":false}',0);
CREATE TABLE metadata_cache (key TEXT PRIMARY KEY, payload BLOB NOT NULL CHECK(json_valid(payload)), expires_at TEXT NOT NULL, schema_version INTEGER NOT NULL CHECK(schema_version>0));
CREATE INDEX episodes_order ON episodes(anime_id,ordinal,id);
CREATE INDEX history_order ON watch_history(last_watched_at DESC,episode_id);
CREATE INDEX bookmarks_order ON bookmarks(created_at DESC,anime_id);
`

func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, ErrInvalid
	}
	dsn := ":memory:"
	if path != ":memory:" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}
		if err = f.Close(); err != nil {
			return nil, err
		}
		if err = os.Chmod(abs, 0600); err != nil {
			return nil, err
		}
		dsn = (&url.URL{Scheme: "file", Path: abs}).String()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// A single connection serializes all writes and preserves connection PRAGMAs.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, q := range []string{"PRAGMA busy_timeout=5000", "PRAGMA foreign_keys=ON", "PRAGMA journal_mode=WAL"} {
		if _, err = db.ExecContext(ctx, q); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err = s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error { return s.db.Close() }
func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY,checksum TEXT NOT NULL,applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(migration))
	checksum := hex.EncodeToString(sum[:])
	rows, err := tx.QueryContext(ctx, "SELECT version,checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return err
	}
	applied := false
	for rows.Next() {
		var version int
		var got string
		if err = rows.Scan(&version, &got); err != nil {
			rows.Close()
			return err
		}
		if version != 1 || got != checksum {
			rows.Close()
			return fmt.Errorf("unsupported or modified database migration %d", version)
		}
		applied = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !applied {
		if _, err = tx.ExecContext(ctx, migration); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations VALUES(1,?,?)", checksum, now()); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func validID(id string) bool {
	return id != "" && len(id) <= 512 && !strings.ContainsAny(id, "\x00\r\n")
}
func normalizeError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (s *Store) UpsertAnime(ctx context.Context, a domain.Anime) error {
	if !validID(a.ID) || strings.TrimSpace(a.Title) == "" {
		return ErrInvalid
	}
	// Only public artwork is durable; capability URLs and credentials are not.
	if a.PosterURL != "" {
		u, err := url.Parse(a.PosterURL)
		if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !(u.Scheme == "https" || u.Scheme == "http" || u.Scheme == "" && strings.HasPrefix(u.Path, "/")) {
			return ErrInvalid
		}
	}
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO anime VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET title=excluded.title,metadata_json=excluded.metadata_json,fetched_at=excluded.fetched_at`, a.ID, a.Title, string(b), now())
	return err
}
func (s *Store) UpsertEpisodes(ctx context.Context, episodes []domain.Episode) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, e := range episodes {
		if !validID(e.ID) || !validID(e.AnimeID) || strings.TrimSpace(e.Label) == "" || math.IsNaN(e.Ordinal) || math.IsInf(e.Ordinal, 0) {
			return ErrInvalid
		}
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO episodes VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET label=excluded.label,ordinal=excluded.ordinal,metadata_json=excluded.metadata_json WHERE episodes.anime_id=excluded.anime_id`, e.ID, e.AnimeID, e.Label, e.Ordinal, string(b))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrConflict
		}
	}
	return tx.Commit()
}
func (s *Store) Anime(ctx context.Context, id string) (domain.Anime, error) {
	var a domain.Anime
	var b string
	err := s.db.QueryRowContext(ctx, "SELECT metadata_json FROM anime WHERE id=?", id).Scan(&b)
	if err != nil {
		return a, normalizeError(err)
	}
	err = json.Unmarshal([]byte(b), &a)
	return a, err
}
func (s *Store) Episodes(ctx context.Context, id string) ([]domain.Episode, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT metadata_json FROM episodes WHERE anime_id=? ORDER BY ordinal,id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.Episode{}
	for rows.Next() {
		var e domain.Episode
		var b string
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(b), &e); err != nil {
			return nil, err
		}
		items = append(items, e)
	}
	return items, rows.Err()
}

type Bookmark struct {
	domain.Anime
	CreatedAt string `json:"createdAt"`
}
type BookmarkPage struct {
	Items      []Bookmark `json:"items"`
	NextCursor *string    `json:"nextCursor"`
}

func (s *Store) PutBookmark(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO bookmarks VALUES(?,?) ON CONFLICT(anime_id) DO NOTHING", id, now())
	return err
}
func (s *Store) DeleteBookmark(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM bookmarks WHERE anime_id=?", id)
	return err
}
func pageArgs(limit int, cursor string) (int, error) {
	if limit < 1 || limit > 50 {
		return 0, ErrInvalid
	}
	if cursor == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(cursor)
	if err != nil || n < 0 {
		return 0, ErrInvalid
	}
	return n, nil
}
func (s *Store) Bookmarks(ctx context.Context, limit int, cursor string) (BookmarkPage, error) {
	p := BookmarkPage{Items: []Bookmark{}}
	offset, err := pageArgs(limit, cursor)
	if err != nil {
		return p, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT a.metadata_json,b.created_at FROM bookmarks b JOIN anime a ON a.id=b.anime_id ORDER BY b.created_at DESC,b.anime_id LIMIT ? OFFSET ?`, limit+1, offset)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var b string
		var item Bookmark
		if err = rows.Scan(&b, &item.CreatedAt); err != nil {
			return p, err
		}
		if err = json.Unmarshal([]byte(b), &item.Anime); err != nil {
			return p, err
		}
		p.Items = append(p.Items, item)
	}
	if len(p.Items) > limit {
		next := strconv.Itoa(offset + limit)
		p.NextCursor = &next
		p.Items = p.Items[:limit]
	}
	return p, rows.Err()
}

type Progress struct {
	EpisodeID       string   `json:"episodeId"`
	PositionSeconds float64  `json:"positionSeconds"`
	DurationSeconds *float64 `json:"durationSeconds"`
	Mode            string   `json:"mode"`
	Completed       bool     `json:"completed"`
	Revision        int64    `json:"revision"`
	UpdatedAt       string   `json:"updatedAt"`
}
type ProgressUpdate struct {
	PositionSeconds float64  `json:"positionSeconds"`
	DurationSeconds *float64 `json:"durationSeconds"`
	Mode            string   `json:"mode"`
	Provider        string   `json:"provider"`
	Revision        int64    `json:"revision"`
	Ended           bool     `json:"ended"`
}
type scanner interface{ Scan(...any) error }

func scanProgress(row scanner) (Progress, error) {
	var p Progress
	err := row.Scan(&p.EpisodeID, &p.PositionSeconds, &p.DurationSeconds, &p.Mode, &p.Completed, &p.Revision, &p.UpdatedAt)
	return p, normalizeError(err)
}

const progressColumns = "episode_id,position,duration,mode,completed,revision,updated_at"

func (s *Store) Progress(ctx context.Context, id string) (Progress, error) {
	return scanProgress(s.db.QueryRowContext(ctx, "SELECT "+progressColumns+" FROM watch_progress WHERE episode_id=?", id))
}
func (s *Store) PutProgress(ctx context.Context, id string, u ProgressUpdate) (Progress, error) {
	if !validID(id) || u.Revision < 0 || math.IsNaN(u.PositionSeconds) || math.IsInf(u.PositionSeconds, 0) || u.PositionSeconds < 0 || (u.Mode != "sub" && u.Mode != "dub") || !providerID(u.Provider) {
		return Progress{}, ErrInvalid
	}
	if u.DurationSeconds != nil && (*u.DurationSeconds < 0 || math.IsNaN(*u.DurationSeconds) || math.IsInf(*u.DurationSeconds, 0)) {
		return Progress{}, ErrInvalid
	}
	if u.DurationSeconds != nil && *u.DurationSeconds == 0 {
		u.DurationSeconds = nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Progress{}, err
	}
	defer tx.Rollback()
	old, err := scanProgress(tx.QueryRowContext(ctx, "SELECT "+progressColumns+" FROM watch_progress WHERE episode_id=?", id))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Progress{}, err
	}
	if old.Revision != u.Revision {
		return Progress{}, ErrConflict
	}
	p := Progress{EpisodeID: id, PositionSeconds: u.PositionSeconds, DurationSeconds: u.DurationSeconds, Mode: u.Mode, Revision: u.Revision + 1, UpdatedAt: now(), Completed: old.Completed || u.Ended}
	if u.DurationSeconds != nil && u.PositionSeconds >= 0.95*(*u.DurationSeconds) {
		p.Completed = true
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO watch_progress VALUES(?,?,?,?,?,?,?) ON CONFLICT(episode_id) DO UPDATE SET position=excluded.position,duration=excluded.duration,mode=excluded.mode,completed=excluded.completed,revision=excluded.revision,updated_at=excluded.updated_at`, id, p.PositionSeconds, p.DurationSeconds, p.Mode, p.Completed, p.Revision, p.UpdatedAt)
	if err != nil {
		return Progress{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO watch_history VALUES(?,?,?,?) ON CONFLICT(episode_id) DO UPDATE SET last_watched_at=excluded.last_watched_at,provider=CASE WHEN excluded.provider='' THEN watch_history.provider ELSE excluded.provider END,mode=excluded.mode`, id, p.UpdatedAt, u.Provider, u.Mode)
	if err != nil {
		return Progress{}, err
	}
	return p, tx.Commit()
}
func providerID(p string) bool {
	if len(p) > 64 {
		return false
	}
	for _, c := range p {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
func (s *Store) ResetProgress(ctx context.Context, id string, revision int64) (Progress, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Progress{}, err
	}
	defer tx.Rollback()
	p, err := scanProgress(tx.QueryRowContext(ctx, "SELECT "+progressColumns+" FROM watch_progress WHERE episode_id=?", id))
	if err != nil {
		return p, err
	}
	if revision != p.Revision {
		return Progress{}, ErrConflict
	}
	p.PositionSeconds = 0
	p.Completed = false
	p.Revision++
	p.UpdatedAt = now()
	_, err = tx.ExecContext(ctx, "UPDATE watch_progress SET position=0,completed=0,revision=?,updated_at=? WHERE episode_id=?", p.Revision, p.UpdatedAt, id)
	if err != nil {
		return Progress{}, err
	}
	return p, tx.Commit()
}

type HistoryItem struct {
	Episode       domain.Episode `json:"episode"`
	Anime         domain.Anime   `json:"anime"`
	Progress      Progress       `json:"progress"`
	Provider      string         `json:"provider"`
	Mode          string         `json:"mode"`
	LastWatchedAt string         `json:"lastWatchedAt"`
}
type HistoryPage struct {
	Items      []HistoryItem `json:"items"`
	NextCursor *string       `json:"nextCursor"`
}

func (s *Store) History(ctx context.Context, limit int, cursor string) (HistoryPage, error) {
	p := HistoryPage{Items: []HistoryItem{}}
	offset, err := pageArgs(limit, cursor)
	if err != nil {
		return p, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.metadata_json,a.metadata_json,h.provider,h.mode,h.last_watched_at,p.episode_id,p.position,p.duration,p.mode,p.completed,p.revision,p.updated_at FROM watch_history h JOIN episodes e ON e.id=h.episode_id JOIN anime a ON a.id=e.anime_id JOIN watch_progress p ON p.episode_id=h.episode_id ORDER BY h.last_watched_at DESC,h.episode_id LIMIT ? OFFSET ?`, limit+1, offset)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var item HistoryItem
		var e, a string
		q := &item.Progress
		if err = rows.Scan(&e, &a, &item.Provider, &item.Mode, &item.LastWatchedAt, &q.EpisodeID, &q.PositionSeconds, &q.DurationSeconds, &q.Mode, &q.Completed, &q.Revision, &q.UpdatedAt); err != nil {
			return p, err
		}
		if err = json.Unmarshal([]byte(e), &item.Episode); err != nil {
			return p, err
		}
		if err = json.Unmarshal([]byte(a), &item.Anime); err != nil {
			return p, err
		}
		p.Items = append(p.Items, item)
	}
	if len(p.Items) > limit {
		next := strconv.Itoa(offset + limit)
		p.NextCursor = &next
		p.Items = p.Items[:limit]
	}
	return p, rows.Err()
}
func (s *Store) ClearHistory(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{"DELETE FROM watch_history", "DELETE FROM watch_progress"} {
		if _, err = tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) ClearBookmarks(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM bookmarks")
	return err
}
func (s *Store) ClearCache(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{"DELETE FROM metadata_cache", "DELETE FROM episode_mappings", "DELETE FROM provider_mappings"} {
		if _, err = tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type Settings struct {
	SubtitleLanguage  string `json:"subtitleLanguage"`
	Quality           string `json:"quality"`
	PreferredProvider string `json:"preferredProvider"`
	Autoplay          bool   `json:"autoplay"`
	Revision          int64  `json:"revision"`
}

func defaults() Settings {
	return Settings{SubtitleLanguage: "id", Quality: "auto", PreferredProvider: "auto"}
}
func scanSettings(row scanner) (Settings, error) {
	var s Settings
	var b string
	var revision int64
	err := row.Scan(&b, &revision)
	if err != nil {
		return s, normalizeError(err)
	}
	err = json.Unmarshal([]byte(b), &s)
	s.Revision = revision
	return s, err
}
func (s *Store) Settings(ctx context.Context) (Settings, error) {
	return scanSettings(s.db.QueryRowContext(ctx, "SELECT value,revision FROM settings WHERE key='preferences'"))
}
func (s *Store) PatchSettings(ctx context.Context, revision int64, patch map[string]json.RawMessage) (Settings, error) {
	return s.changeSettings(ctx, revision, patch, false)
}
func (s *Store) ResetSettings(ctx context.Context, revision int64) (Settings, error) {
	return s.changeSettings(ctx, revision, nil, true)
}
func (s *Store) changeSettings(ctx context.Context, revision int64, patch map[string]json.RawMessage, reset bool) (Settings, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Settings{}, err
	}
	defer tx.Rollback()
	v, err := scanSettings(tx.QueryRowContext(ctx, "SELECT value,revision FROM settings WHERE key='preferences'"))
	if err != nil {
		return v, err
	}
	if revision != v.Revision {
		return Settings{}, ErrConflict
	}
	if reset {
		v = defaults()
	}
	for k, b := range patch {
		if string(b) == "null" {
			return Settings{}, ErrInvalid
		}
		switch k {
		case "subtitleLanguage":
			err = json.Unmarshal(b, &v.SubtitleLanguage)
		case "quality":
			err = json.Unmarshal(b, &v.Quality)
		case "preferredProvider":
			err = json.Unmarshal(b, &v.PreferredProvider)
		case "autoplay":
			err = json.Unmarshal(b, &v.Autoplay)
		default:
			return Settings{}, ErrInvalid
		}
		if err != nil {
			return Settings{}, ErrInvalid
		}
	}
	if v.SubtitleLanguage != "id" && v.SubtitleLanguage != "en" && v.SubtitleLanguage != "off" {
		return Settings{}, ErrInvalid
	}
	if v.Quality != "auto" && v.Quality != "360p" && v.Quality != "480p" && v.Quality != "720p" && v.Quality != "1080p" {
		return Settings{}, ErrInvalid
	}
	if v.PreferredProvider != "auto" && v.PreferredProvider != "hianime" && v.PreferredProvider != "otakudesu" && v.PreferredProvider != "anichan" {
		return Settings{}, ErrInvalid
	}
	v.Revision = revision + 1
	b, err := json.Marshal(v)
	if err != nil {
		return Settings{}, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE settings SET value=?,revision=? WHERE key='preferences'", string(b), v.Revision)
	if err != nil {
		return Settings{}, err
	}
	return v, tx.Commit()
}
