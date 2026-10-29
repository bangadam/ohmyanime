package store

import "context"

// Manual watched flags live in their own table so an externally watched
// episode never needs an invented playback position or duration. The
// effective "watched" value combines both sources with explicit
// precedence:
//
//  1. A manual assertion wins over automatic completion. state=1 forces
//     watched; state=-1 (explicit unwatch) forces unwatched even when the
//     episode previously reached 95% or ended.
//  2. Without a manual assertion, the automatic completion recorded in
//     watch_progress decides.
//  3. Automatic events take the flag back over: a later automatic
//     completion (PutProgress reaching the end) and an explicit progress
//     reset (ResetProgress) both clear the manual row, so a rewatch or a
//     reset behaves as expected instead of being pinned to an old manual
//     assertion. Partial progress updates never clear the manual flag,
//     which keeps positions intact when toggling (the flag is stored
//     apart from watch_progress, so toggling cannot move a resume point).

// SetWatched idempotently records a manual watched (watched=true) or
// unwatched (watched=false) assertion for one episode. Playback positions
// and history are untouched. The returned Progress reports the effective
// state after the change; when no progress row exists it is synthesized
// with zero position, no duration and revision 1 so playback can start
// with the normal conflict protocol.
func (s *Store) SetWatched(ctx context.Context, episodeID string, watched bool) (Progress, error) {
	if !validID(episodeID) {
		return Progress{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Progress{}, err
	}
	defer tx.Rollback()
	var animeID string
	if err = tx.QueryRowContext(ctx, "SELECT anime_id FROM episodes WHERE id=?", episodeID).Scan(&animeID); err != nil {
		return Progress{}, normalizeError(err)
	}
	state := -1
	if watched {
		state = 1
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO manual_watched VALUES(?,?,?) ON CONFLICT(episode_id) DO UPDATE SET state=excluded.state,set_at=excluded.set_at`, episodeID, state, now()); err != nil {
		return Progress{}, err
	}
	if err = tx.Commit(); err != nil {
		return Progress{}, err
	}
	return s.Progress(ctx, episodeID)
}

// SetWatchedBulk idempotently applies a manual watched assertion to every
// episode of one title in the real episode inventory (the episodes table).
// It returns the number of episodes affected; a title with no stored
// inventory is a no-op so bulk actions never invent episodes. Each anime
// record represents a single season, so title scope is season scope here.
// Bulk state never touches other titles.
func (s *Store) SetWatchedBulk(ctx context.Context, animeID string, watched bool) (int, error) {
	if !validID(animeID) {
		return 0, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT id FROM episodes WHERE anime_id=? ORDER BY ordinal,id", animeID)
	if err != nil {
		return 0, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	state := -1
	if watched {
		state = 1
	}
	ts := now()
	for _, id := range ids {
		if _, err = tx.ExecContext(ctx, `INSERT INTO manual_watched VALUES(?,?,?) ON CONFLICT(episode_id) DO UPDATE SET state=excluded.state,set_at=excluded.set_at`, id, state, ts); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return len(ids), nil
}

// WatchedState is the effective watched flag for one episode.
type WatchedState struct {
	EpisodeID string `json:"episodeId"`
	Watched   bool   `json:"watched"`
}

func (s *Store) WatchedStates(ctx context.Context, animeID string) ([]WatchedState, error) {
	if !validID(animeID) {
		return nil, ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.id,CASE WHEN m.state=1 THEN 1 WHEN m.state=-1 THEN 0 ELSE COALESCE(p.completed,0) END FROM episodes e LEFT JOIN watch_progress p ON p.episode_id=e.id LEFT JOIN manual_watched m ON m.episode_id=e.id WHERE e.anime_id=? ORDER BY e.ordinal,e.id`, animeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []WatchedState{}
	for rows.Next() {
		var item WatchedState
		var watched int
		if err = rows.Scan(&item.EpisodeID, &watched); err != nil {
			return nil, err
		}
		item.Watched = watched == 1
		items = append(items, item)
	}
	return items, rows.Err()
}
