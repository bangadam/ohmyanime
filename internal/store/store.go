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

type Store struct {
	db   *sql.DB
	path string // absolute database file path, or ":memory:"
}

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

// migration2 is additive only (new table) and therefore backward-safe for
// databases created at version 1. Each migration is immutable after release:
// its SHA-256 is checked on every open, so fixes must ship as a new version.
const migration2 = `
CREATE TABLE continue_hidden (anime_id TEXT PRIMARY KEY REFERENCES anime(id), hidden_at TEXT NOT NULL);
`

// migration3 is additive only (new manga tables) and therefore backward-safe
// for databases created at version 1 or 2: anime rows, bookmarks and watch
// progress are untouched. As above, the text is immutable after release.
const migration3 = `
CREATE TABLE manga (id TEXT PRIMARY KEY, title TEXT NOT NULL, metadata_json TEXT NOT NULL CHECK(json_valid(metadata_json)), fetched_at TEXT NOT NULL);
CREATE TABLE chapters (id TEXT PRIMARY KEY, manga_id TEXT NOT NULL REFERENCES manga(id), label TEXT NOT NULL, ordinal REAL NOT NULL, metadata_json TEXT NOT NULL CHECK(json_valid(metadata_json)), UNIQUE(manga_id,label));
CREATE TABLE manga_provider_mappings (provider TEXT NOT NULL, manga_id TEXT NOT NULL REFERENCES manga(id), provider_ref TEXT NOT NULL, evidence_json TEXT NOT NULL CHECK(json_valid(evidence_json)), verified_at TEXT NOT NULL, PRIMARY KEY(provider,manga_id));
CREATE TABLE chapter_provider_mappings (provider TEXT NOT NULL, chapter_id TEXT NOT NULL REFERENCES chapters(id), provider_chapter_ref TEXT NOT NULL, PRIMARY KEY(provider,chapter_id));
CREATE TABLE reading_progress (manga_id TEXT PRIMARY KEY REFERENCES manga(id), chapter_id TEXT NOT NULL REFERENCES chapters(id), page INTEGER NOT NULL CHECK(page>=0), completed INTEGER NOT NULL CHECK(completed IN (0,1)), revision INTEGER NOT NULL CHECK(revision>0), updated_at TEXT NOT NULL);
CREATE INDEX chapters_order ON chapters(manga_id,ordinal,id);
`

// migration4 is additive only (new table) and therefore backward-safe for
// databases created at version 1, 2 or 3. As above, the text is immutable
// after release. state is tri-stated: 1 marks watched, -1 an explicit
// unwatch that overrides automatic completion (precedence in watched.go).
const migration4 = `
CREATE TABLE manual_watched (episode_id TEXT PRIMARY KEY REFERENCES episodes(id), state INTEGER NOT NULL CHECK(state IN (-1,1)), set_at TEXT NOT NULL);
`

// migration5 is additive only (new table) and therefore backward-safe for
// databases created at version 1-4. Manga bookmarks mirror anime bookmarks
// with their own namespaced ids ("mangadex:..."), so the two never collide.
const migration5 = `
CREATE TABLE manga_bookmarks (manga_id TEXT PRIMARY KEY REFERENCES manga(id), created_at TEXT NOT NULL);
CREATE INDEX manga_bookmarks_order ON manga_bookmarks(created_at DESC,manga_id);
`

var migrations = []string{migration, migration2, migration3, migration4, migration5}

func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, ErrInvalid
	}
	dsn := ":memory:"
	libPath := ":memory:"
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
		libPath = abs
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// A single connection serializes all writes and preserves connection PRAGMAs.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db, path: libPath}
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
	rows, err := tx.QueryContext(ctx, "SELECT version,checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return err
	}
	applied := map[int]string{}
	for rows.Next() {
		var version int
		var got string
		if err = rows.Scan(&version, &got); err != nil {
			rows.Close()
			return err
		}
		if version < 1 || version > len(migrations) {
			rows.Close()
			return fmt.Errorf("unsupported or modified database migration %d", version)
		}
		applied[version] = got
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for i, text := range migrations {
		version := i + 1
		sum := sha256.Sum256([]byte(text))
		checksum := hex.EncodeToString(sum[:])
		if got, ok := applied[version]; ok {
			if got != checksum {
				return fmt.Errorf("unsupported or modified database migration %d", version)
			}
			continue
		}
		if _, err = tx.ExecContext(ctx, text); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations VALUES(?,?,?)", version, checksum, now()); err != nil {
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

func (s *Store) UpsertManga(ctx context.Context, m domain.Manga) error {
	if !validID(m.ID) || strings.TrimSpace(m.Title) == "" {
		return ErrInvalid
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO manga VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET title=excluded.title,metadata_json=excluded.metadata_json,fetched_at=excluded.fetched_at`, m.ID, m.Title, string(b), now())
	return err
}

func (s *Store) UpsertChapters(ctx context.Context, chapters []domain.Chapter) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, c := range chapters {
		if !validID(c.ID) || !validID(c.MangaID) || strings.TrimSpace(c.Label) == "" || math.IsNaN(c.Ordinal) || math.IsInf(c.Ordinal, 0) {
			return ErrInvalid
		}
		b, err := json.Marshal(c)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO chapters VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET label=excluded.label,ordinal=excluded.ordinal,metadata_json=excluded.metadata_json WHERE chapters.manga_id=excluded.manga_id`, c.ID, c.MangaID, c.Label, c.Ordinal, string(b))
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

func (s *Store) Manga(ctx context.Context, id string) (domain.Manga, error) {
	var m domain.Manga
	var b string
	if err := s.db.QueryRowContext(ctx, "SELECT metadata_json FROM manga WHERE id=?", id).Scan(&b); err != nil {
		return m, normalizeError(err)
	}
	err := json.Unmarshal([]byte(b), &m)
	return m, err
}

func (s *Store) Chapters(ctx context.Context, id string) ([]domain.Chapter, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT metadata_json FROM chapters WHERE manga_id=? ORDER BY ordinal,id", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.Chapter{}
	for rows.Next() {
		var c domain.Chapter
		var b string
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(b), &c); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

// ReadingProgressUpdate sets the resumable position for one manga. Progress
// is per manga: it points at the chapter last read and the page reached.
// ExpectedRevision guards concurrent windows: when non-nil a write only
// succeeds if it matches the stored revision, so a stale window cannot
// overwrite newer progress. A zero value of *int64 is indistinguishable from
// nil, so the API layer passes a pointer to a real revision (0 included).
type ReadingProgressUpdate struct {
	ChapterID        string
	Page             int
	Completed        bool
	ExpectedRevision *int64
}

// ReadingProgressState is the stored reading position plus its revision for
// optimistic concurrency, mirroring anime watch progress.
type ReadingProgressState struct {
	MangaID   string `json:"mangaId"`
	ChapterID string `json:"chapterId"`
	Page      int    `json:"page"`
	Completed bool   `json:"completed"`
	Revision  int64  `json:"revision"`
	UpdatedAt string `json:"updatedAt"`
}

func (s *Store) ReadingProgress(ctx context.Context, mangaID string) (ReadingProgressState, error) {
	var p ReadingProgressState
	err := s.db.QueryRowContext(ctx, `SELECT manga_id,chapter_id,page,completed,revision,updated_at FROM reading_progress WHERE manga_id=?`, mangaID).
		Scan(&p.MangaID, &p.ChapterID, &p.Page, &p.Completed, &p.Revision, &p.UpdatedAt)
	if err != nil {
		return p, normalizeError(err)
	}
	return p, nil
}

// PutReadingProgress inserts or updates the reading position for one manga.
// The referenced chapter must already exist in the chapters table. The page
// is clamped to the chapter's stored page count so a shrunken inventory can
// never produce an out-of-range position.
func (s *Store) PutReadingProgress(ctx context.Context, mangaID string, u ReadingProgressUpdate) (ReadingProgressState, error) {
	if !validID(mangaID) || !validID(u.ChapterID) || u.Page < 0 {
		return ReadingProgressState{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ReadingProgressState{}, err
	}
	defer tx.Rollback()
	var current ReadingProgressState
	err = tx.QueryRowContext(ctx, `SELECT manga_id,chapter_id,page,completed,revision,updated_at FROM reading_progress WHERE manga_id=?`, mangaID).
		Scan(&current.MangaID, &current.ChapterID, &current.Page, &current.Completed, &current.Revision, &current.UpdatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ReadingProgressState{}, err
	}
	if u.ExpectedRevision != nil && (err == nil && *u.ExpectedRevision != current.Revision || err != nil && *u.ExpectedRevision != 0) {
		return ReadingProgressState{}, ErrConflict
	}
	// Clamp to the current inventory: the provider may have removed pages
	// since the reader last saved.
	var pageCount int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(json_extract(metadata_json,'$.pageCount'),0) FROM chapters WHERE id=? AND manga_id=?`, u.ChapterID, mangaID).Scan(&pageCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ReadingProgressState{}, ErrInvalid
		}
		return ReadingProgressState{}, err
	}
	page := u.Page
	if pageCount > 0 && page >= pageCount {
		page = pageCount - 1
	}
	ts := now()
	if err == nil {
		if _, err = tx.ExecContext(ctx, `UPDATE reading_progress SET chapter_id=?,page=?,completed=?,revision=revision+1,updated_at=? WHERE manga_id=?`, u.ChapterID, page, u.Completed, ts, mangaID); err != nil {
			return ReadingProgressState{}, err
		}
	} else if _, err = tx.ExecContext(ctx, `INSERT INTO reading_progress VALUES(?,?,?,?,1,?)`, mangaID, u.ChapterID, page, u.Completed, ts); err != nil {
		return ReadingProgressState{}, err
	}
	if err = tx.Commit(); err != nil {
		return ReadingProgressState{}, err
	}
	return s.ReadingProgress(ctx, mangaID)
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

type MangaBookmark struct {
	domain.Manga
	CreatedAt string `json:"createdAt"`
}
type MangaBookmarkPage struct {
	Items      []MangaBookmark `json:"items"`
	NextCursor *string         `json:"nextCursor"`
}

func (s *Store) PutMangaBookmark(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO manga_bookmarks VALUES(?,?) ON CONFLICT(manga_id) DO NOTHING", id, now())
	return err
}
func (s *Store) DeleteMangaBookmark(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM manga_bookmarks WHERE manga_id=?", id)
	return err
}
func (s *Store) MangaBookmarks(ctx context.Context, limit int, cursor string) (MangaBookmarkPage, error) {
	p := MangaBookmarkPage{Items: []MangaBookmark{}}
	offset, err := pageArgs(limit, cursor)
	if err != nil {
		return p, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.metadata_json,b.created_at FROM manga_bookmarks b JOIN manga m ON m.id=b.manga_id ORDER BY b.created_at DESC,b.manga_id LIMIT ? OFFSET ?`, limit+1, offset)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var b string
		var item MangaBookmark
		if err = rows.Scan(&b, &item.CreatedAt); err != nil {
			return p, err
		}
		if err = json.Unmarshal([]byte(b), &item.Manga); err != nil {
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

// Progress reports the effective state of one episode. Manual watched
// flags (see watched.go) take precedence over automatic completion; when
// only a manual flag exists a synthesized zero-position progress row is
// returned so playback can start without an invented duration.
func (s *Store) Progress(ctx context.Context, id string) (Progress, error) {
	p, err := scanProgress(s.db.QueryRowContext(ctx, "SELECT p.episode_id,p.position,p.duration,p.mode,CASE WHEN m.state=1 THEN 1 WHEN m.state=-1 THEN 0 ELSE p.completed END,p.revision,p.updated_at FROM watch_progress p LEFT JOIN manual_watched m ON m.episode_id=p.episode_id WHERE p.episode_id=?", id))
	if errors.Is(err, ErrNotFound) {
		var state int
		var setAt string
		if e := s.db.QueryRowContext(ctx, "SELECT state,set_at FROM manual_watched WHERE episode_id=?", id).Scan(&state, &setAt); e == nil {
			return Progress{EpisodeID: id, Mode: "sub", Completed: state == 1, Revision: 1, UpdatedAt: setAt}, nil
		}
	}
	return p, err
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
	// A "Remove from Continue watching" only hides the title from the resume
	// shelf; history and playback position stay intact. Watching the title
	// again (any progress update for one of its episodes) un-hides it so the
	// shelf reflects renewed interest.
	if _, err = tx.ExecContext(ctx, `DELETE FROM continue_hidden WHERE anime_id=(SELECT anime_id FROM episodes WHERE id=?)`, id); err != nil {
		return Progress{}, err
	}
	// Precedence (see watched.go): an automatic completion takes the
	// flag back from any manual assertion, so a finished rewatch or an
	// "ended" report is not pinned to an old manual state.
	if p.Completed {
		if _, err = tx.ExecContext(ctx, `DELETE FROM manual_watched WHERE episode_id=?`, id); err != nil {
			return Progress{}, err
		}
	}
	return p, tx.Commit()
}

func (s *Store) HideContinue(ctx context.Context, animeID string) error {
	if !validID(animeID) {
		return ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO continue_hidden VALUES(?,?) ON CONFLICT(anime_id) DO UPDATE SET hidden_at=excluded.hidden_at`, animeID, now())
	return err
}

func (s *Store) ContinueHidden(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT anime_id FROM continue_hidden ORDER BY hidden_at,anime_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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
	// An explicit progress reset also clears any manual watched assertion,
	// so "watch from the beginning" is not pinned to an old manual state
	// (precedence documented in watched.go).
	if _, err = tx.ExecContext(ctx, `DELETE FROM manual_watched WHERE episode_id=?`, id); err != nil {
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
	rows, err := s.db.QueryContext(ctx, `SELECT e.metadata_json,a.metadata_json,h.provider,h.mode,h.last_watched_at,p.episode_id,p.position,p.duration,p.mode,CASE WHEN m.state=1 THEN 1 WHEN m.state=-1 THEN 0 ELSE p.completed END,p.revision,p.updated_at FROM watch_history h JOIN episodes e ON e.id=h.episode_id JOIN anime a ON a.id=e.anime_id JOIN watch_progress p ON p.episode_id=h.episode_id LEFT JOIN manual_watched m ON m.episode_id=h.episode_id ORDER BY h.last_watched_at DESC,h.episode_id LIMIT ? OFFSET ?`, limit+1, offset)
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{"DELETE FROM bookmarks", "DELETE FROM manga_bookmarks"} {
		if _, err = tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return tx.Commit()
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
