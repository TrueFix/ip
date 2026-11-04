// Package audit persists provider-update history only. It stores no visitor IPs,
// request logs, provider credentials, or credential-bearing download URLs.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/TrueFix/myip/internal/inventory"
	_ "modernc.org/sqlite"
)

const Filename = "state.sqlite"

type DB struct{ db *sql.DB }
type Run struct {
	ID         int64  `json:"id"`
	Provider   string `json:"provider"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	Status     string `json:"status"`
	Snapshot   string `json:"snapshot"`
	ErrorCode  string `json:"error_code,omitempty"`
}

func Open(dir string) (*DB, error) {
	path, err := filepath.Abs(filepath.Join(dir, Filename))
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	f.Close()
	u := url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite", u.String()+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(err error) (*DB, error) { db.Close(); return nil, err }
	if _, err = db.Exec(`PRAGMA journal_mode=DELETE; PRAGMA synchronous=FULL;`); err != nil {
		return fail(err)
	}
	var version int
	if err = db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fail(err)
	}
	if version > 1 {
		return fail(errors.New("unsupported audit schema version"))
	}
	var integrity string
	if err = db.QueryRow(`PRAGMA quick_check`).Scan(&integrity); err != nil || integrity != "ok" {
		return fail(errors.New("audit SQLite integrity check failed"))
	}
	_, err = db.Exec(`BEGIN;
CREATE TABLE IF NOT EXISTS update_runs (
 id INTEGER PRIMARY KEY, provider TEXT NOT NULL, started_at TEXT NOT NULL,
 finished_at TEXT NOT NULL DEFAULT '', status TEXT NOT NULL,
 snapshot TEXT NOT NULL DEFAULT '', error_code TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS database_files (
 run_id INTEGER NOT NULL REFERENCES update_runs(id) ON DELETE CASCADE,
 provider TEXT NOT NULL, edition TEXT NOT NULL, path TEXT NOT NULL,
 sha256 TEXT NOT NULL, size_bytes INTEGER NOT NULL, build_epoch INTEGER NOT NULL,
 downloaded_at TEXT NOT NULL, checked_at TEXT NOT NULL, verified_at TEXT NOT NULL,
 etag TEXT NOT NULL, last_modified TEXT NOT NULL, source_ref TEXT NOT NULL,
 action TEXT NOT NULL, metadata_json TEXT NOT NULL,
 PRIMARY KEY(run_id,path)
);
PRAGMA user_version=1;
COMMIT;`)
	if err != nil {
		return fail(err)
	}
	return &DB{db}, nil
}
func (d *DB) Close() error { return d.db.Close() }
func (d *DB) Begin(provider string) (int64, error) {
	r, err := d.db.Exec(`INSERT INTO update_runs(provider,started_at,status) VALUES(?,?,'running')`, provider, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func (d *DB) Recover(version string, id int64) error {
	if id > 0 {
		var count int
		if err := d.db.QueryRow(`SELECT COUNT(*) FROM update_runs WHERE id=? AND snapshot=?`, id, version).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return errors.New("published snapshot audit record is missing; restore audit history before updating")
		}
	}
	_, err := d.db.Exec(`UPDATE update_runs SET status=CASE WHEN id=? AND snapshot=? THEN 'success' ELSE 'interrupted' END, finished_at=? WHERE status IN ('running','prepared')`, id, version, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (d *DB) Prepare(id int64, version string, files []inventory.File) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE update_runs SET status='prepared',snapshot=? WHERE id=?`, version, id); err != nil {
		return err
	}
	for _, f := range files {
		b, err := json.Marshal(f)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO database_files(run_id,provider,edition,path,sha256,size_bytes,build_epoch,downloaded_at,checked_at,verified_at,etag,last_modified,source_ref,action,metadata_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, f.Provider, f.Edition, f.Path, f.SHA256, f.Size, f.BuildEpoch, stamp(f.DownloadedAt), stamp(f.CheckedAt), stamp(f.VerifiedAt), f.ETag, f.LastModified, f.SourceRef, f.Action, string(b))
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func (d *DB) Finish(id int64, success bool, publishedID int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status, code := "success", ""
	if !success {
		status, code = "failed", "update_failed"
	}
	if _, err := d.db.ExecContext(ctx, `UPDATE update_runs SET status=?,error_code=?,finished_at=? WHERE id=?`, status, code, time.Now().UTC().Format(time.RFC3339Nano), id); err != nil {
		return err
	}
	_, err := d.db.ExecContext(ctx, `DELETE FROM update_runs WHERE id<>? AND id NOT IN (SELECT id FROM update_runs ORDER BY id DESC LIMIT 1000)`, publishedID)
	return err
}

// History uses a read-only connection and never migrates or creates a database.
func History(dir string) ([]Run, error) {
	path, err := filepath.Abs(filepath.Join(dir, Filename))
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite", u.String()+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := db.QueryContext(ctx, `SELECT id,provider,started_at,finished_at,status,snapshot,error_code FROM update_runs ORDER BY id DESC LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Run{}
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.Provider, &r.StartedAt, &r.FinishedAt, &r.Status, &r.Snapshot, &r.ErrorCode); err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}
