package updater

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TrueFix/myip/internal/audit"
	"github.com/TrueFix/myip/internal/geo"
)

func TestDiscardUnpublishedSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name, manifest string
		keep           bool
	}{
		{"old-active", `{"version":"old-snapshot"}`, false},
		{"first-publication-failed", "", false},
		{"rename-completed-before-error", `{"version":"new-snapshot"}`, true},
		{"malformed-manifest", `{`, true},
		{"invalid-manifest-version", `{"version":"../outside"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			candidate := filepath.Join(dir, "new-snapshot")
			if err := os.Mkdir(candidate, 0755); err != nil {
				t.Fatal(err)
			}
			if tc.manifest != "" {
				if err := os.WriteFile(filepath.Join(dir, "current.json"), []byte(tc.manifest), 0644); err != nil {
					t.Fatal(err)
				}
			}
			u := Updater{Dir: dir}
			u.discardUnpublished("new-snapshot")
			_, err := os.Stat(candidate)
			if tc.keep && err != nil {
				t.Fatal("potentially active snapshot deleted", err)
			}
			if !tc.keep && !os.IsNotExist(err) {
				t.Fatal("unpublished snapshot not removed", err)
			}
		})
	}
}

func TestAuditPrepareFailureDoesNotLeaveOrphanSnapshot(t *testing.T) {
	dir := t.TempDir()
	u := Updater{Dir: dir, Provider: "dbip", Client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		fixture := "GeoIP2-City-Test.mmdb"
		if strings.Contains(r.URL.Path, "asn") {
			fixture = "GeoLite2-ASN-Test.mmdb"
		}
		data, err := os.ReadFile(filepath.Join("..", "geo", "testdata", fixture))
		if err != nil {
			return nil, err
		}
		var body bytes.Buffer
		z := gzip.NewWriter(&body)
		if _, err := z.Write(data); err != nil {
			return nil, err
		}
		if err := z.Close(); err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(&body)}, nil
	})}}
	if err := u.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "current.json"))
	if err != nil {
		t.Fatal(err)
	}
	var current geo.Manifest
	if err := json.Unmarshal(before, &current); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, audit.Filename))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TRIGGER reject_prepare BEFORE UPDATE OF status ON update_runs WHEN NEW.status='prepared' BEGIN SELECT RAISE(FAIL, 'injected prepare failure'); END`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := u.Run(context.Background()); err == nil {
		t.Fatal("injected prepare failure was ignored")
	}
	after, err := os.ReadFile(filepath.Join(dir, "current.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed preparation changed serving manifest", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != current.Version {
			t.Fatalf("failed update left orphan directory %s", entry.Name())
		}
	}
}
