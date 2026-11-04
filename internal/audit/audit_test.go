package audit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/TrueFix/myip/internal/inventory"
)

func TestAuditHistoryAndCrashRecovery(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.Begin("dbip")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Prepare(id, "snapshot-1", []inventory.File{{Provider: "dbip", Edition: "city", Path: "City.mmdb", SHA256: "test-hash"}}); err != nil {
		t.Fatal(err)
	}
	if err := db.Recover("snapshot-1", id); err != nil {
		t.Fatal(err)
	}
	other, err := db.Begin("maxmind")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Prepare(other, "unpublished", nil); err != nil {
		t.Fatal(err)
	}
	if err := db.Recover("snapshot-1", id); err != nil {
		t.Fatal(err)
	}
	db.Close()
	runs, err := History(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].Status != "interrupted" || runs[1].Status != "success" {
		t.Fatalf("bad recovery history: %+v", runs)
	}
	b, err := os.ReadFile(filepath.Join(dir, Filename))
	if err != nil || string(b[:15]) != "SQLite format 3" {
		t.Fatal("not a real SQLite database")
	}
}

func TestReadOnlyHistoryDoesNotCreateDatabase(t *testing.T) {
	dir := t.TempDir()
	if _, err := History(dir); err == nil {
		t.Fatal("missing history accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, Filename)); !os.IsNotExist(err) {
		t.Fatal("read-only history created state")
	}
}

func TestMissingPublishedAuditIsNotSilentlyRecreated(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if db.Recover("published", 42) == nil {
		t.Fatal("missing published history accepted")
	}
}

func TestRetentionPreservesPublishedSnapshot(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	active, err := db.Begin("dbip")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Prepare(active, "active", nil); err != nil {
		t.Fatal(err)
	}
	tx, err := db.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	var last int64
	for range 1005 {
		r, err := tx.Exec(`INSERT INTO update_runs(provider,started_at,status) VALUES('dbip','test','failed')`)
		if err != nil {
			t.Fatal(err)
		}
		last, _ = r.LastInsertId()
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := db.Finish(last, false, active); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM update_runs`).Scan(&count); err != nil || count != 1001 {
		t.Fatal("history retention not bounded")
	}
	if err := db.Recover("active", active); err != nil {
		t.Fatal("active audit row pruned", err)
	}
}
