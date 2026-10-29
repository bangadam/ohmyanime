package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrIncompatibleImport reports an import file that is not a valid library
// database, carries an unsupported schema version, or fails integrity checks.
var ErrIncompatibleImport = errors.New("incompatible or damaged import")

// Export writes a consistent snapshot of the whole library to dest using
// SQLite's VACUUM INTO backup. VACUUM INTO reads a stable snapshot inside its
// own transaction, so active writes can never corrupt the export and no live
// WAL file is ever copied. An existing dest file is replaced.
func (s *Store) Export(ctx context.Context, dest string) error {
	if strings.TrimSpace(dest) == "" || dest == ":memory:" {
		return ErrInvalid
	}
	abs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	if abs == s.path {
		return ErrInvalid
	}
	if err = os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return err
	}
	// VACUUM INTO refuses to overwrite a non-empty target, so a stale export
	// is removed first; the write itself is atomic.
	if err = os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, err = s.db.ExecContext(ctx, "VACUUM INTO "+sqliteString(abs)); err != nil {
		return err
	}
	return os.Chmod(abs, 0600)
}

// Import validates src as a compatible, undamaged library, creates a
// pre-restore backup of the current data, and swaps the library file in
// place. All validation happens on a staged copy before the live database is
// touched. If the swap or the reopen fails, the original library is rolled
// back from the pre-restore backup, so a failed restore always leaves the
// original data usable. On success the backup remains next to the library as
// <library>.pre-restore.
func (s *Store) Import(ctx context.Context, src string) error {
	if strings.TrimSpace(src) == "" {
		return ErrInvalid
	}
	if s.path == ":memory:" {
		return ErrInvalid
	}
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	if srcAbs == s.path {
		return ErrInvalid
	}
	info, err := os.Stat(srcAbs)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return ErrInvalid
	}
	dir := filepath.Dir(s.path)
	tmp := filepath.Join(dir, filepath.Base(s.path)+".import-tmp")
	backup := filepath.Join(dir, filepath.Base(s.path)+".pre-restore")

	// Stage a copy and validate the copy, not the source: a source file being
	// modified concurrently cannot sneak past validation, and nothing in this
	// phase touches the live database.
	if err = os.Remove(tmp); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err = copyFile(srcAbs, tmp); err != nil {
		return err
	}
	if err = verifyLibrary(tmp); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("%w: %v", ErrIncompatibleImport, err)
	}

	// Pre-restore backup: a consistent snapshot of the current data, taken
	// before anything is replaced.
	if err = os.Remove(backup); err != nil && !os.IsNotExist(err) {
		os.Remove(tmp)
		return err
	}
	if _, err = s.db.ExecContext(ctx, "VACUUM INTO "+sqliteString(backup)); err != nil {
		os.Remove(tmp)
		return err
	}
	if err = os.Chmod(backup, 0600); err != nil {
		os.Remove(tmp)
		return err
	}
	// Flush the WAL into the main file so the swap needs no sidecar files.
	if _, err = s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		os.Remove(tmp)
		return err
	}
	if err = s.db.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err = swapLibrary(s.path, tmp, backup); err != nil {
		s.reopen(ctx) // swapLibrary already rolled back to the original data
		os.Remove(tmp)
		return err
	}
	// Reopen the replaced library; roll back from the backup if it fails.
	if err = s.reopen(ctx); err != nil {
		os.Remove(s.path)
		os.Remove(s.path + "-wal")
		os.Remove(s.path + "-shm")
		if rerr := os.Rename(backup, s.path); rerr != nil {
			os.Remove(tmp)
			return fmt.Errorf("import failed (%v) and restore of the original failed: %w", err, rerr)
		}
		s.reopen(ctx)
		os.Remove(tmp)
		return err
	}
	return nil
}

// swapLibrary installs tmp as the new library file, rolling back from backup
// if the install fails.
func swapLibrary(path, tmp, backup string) error {
	// Stale sidecars from the closed connection: after the TRUNCATE
	// checkpoint the main file already holds every committed write.
	for _, side := range []string{path + "-wal", path + "-shm"} {
		if err := os.Remove(side); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		if rerr := copyFile(backup, path); rerr != nil {
			return fmt.Errorf("import failed (%v) and rollback failed: %w", err, rerr)
		}
		return err
	}
	return nil
}

// reopen re-establishes the connection, PRAGMAs and migrations on the current
// s.path after a file-level swap.
func (s *Store) reopen(ctx context.Context) error {
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: s.path}).String())
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s.db = db
	for _, q := range []string{"PRAGMA busy_timeout=5000", "PRAGMA foreign_keys=ON", "PRAGMA journal_mode=WAL"} {
		if _, err = db.ExecContext(ctx, q); err != nil {
			db.Close()
			return err
		}
	}
	if err = s.migrate(ctx); err != nil {
		db.Close()
		return err
	}
	return nil
}

// verifyLibrary checks that path is a readable SQLite database with an intact
// logical structure and a schema version this build understands. The file is
// opened read-only, so verification never mutates it.
func verifyLibrary(path string) error {
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var check string
	if err = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&check); err != nil {
		return fmt.Errorf("integrity check: %w", err)
	}
	if check != "ok" {
		return fmt.Errorf("integrity check: %s", check)
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	violation := rows.Next()
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if violation {
		return errors.New("foreign key violations")
	}
	rows, err = db.QueryContext(ctx, "SELECT version,checksum FROM schema_migrations ORDER BY version")
	if err != nil {
		return fmt.Errorf("missing or unreadable schema_migrations: %w", err)
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
			return fmt.Errorf("unsupported migration version %d", version)
		}
		applied[version] = got
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		return errors.New("no migrations applied")
	}
	for i, text := range migrations {
		version := i + 1
		if got, ok := applied[version]; ok {
			sum := sha256.Sum256([]byte(text))
			if got != hex.EncodeToString(sum[:]) {
				return fmt.Errorf("migration %d checksum mismatch", version)
			}
		}
		// A library older than this build is accepted: the additive
		// migrations apply on reopen, exactly like a normal Open.
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err = out.Sync(); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

// sqliteString renders s as a single-quoted SQL string literal.
func sqliteString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
