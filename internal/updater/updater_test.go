package updater

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TrueFix/myip/internal/audit"
	"github.com/TrueFix/myip/internal/geo"
	"github.com/TrueFix/myip/internal/inventory"
	"github.com/gofrs/flock"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDBIPPublicationAndConditionalRefresh(t *testing.T) {
	calls := 0
	release := "1"
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		name := "GeoIP2-City-Test.mmdb"
		if strings.Contains(r.URL.Path, "asn") {
			name = "GeoLite2-ASN-Test.mmdb"
		}
		etag := `"` + name + release + `"`
		if r.Header.Get("If-None-Match") == etag {
			return &http.Response{StatusCode: 304, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Etag": []string{etag}}}, nil
		}
		b, err := os.ReadFile(filepath.Join("..", "geo", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		var compressed bytes.Buffer
		z := gzip.NewWriter(&compressed)
		_, _ = z.Write(b)
		_ = z.Close()
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(compressed.Bytes())), Header: http.Header{"Etag": []string{etag}}}, nil
	})}
	dir := t.TempDir()
	u := Updater{Dir: dir, Provider: "dbip", Client: client}
	if err := u.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	store := geo.NewStore(dir, "dbip")
	defer store.Close()
	if err := store.Reload(); err != nil {
		t.Fatal(err)
	}
	if !store.Ready() || calls != 2 {
		t.Fatalf("not ready or wrong download count: %d", calls)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "current.json"))
	if err := u.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "current.json"))
	if calls != 4 {
		t.Fatal("same-month files were not checked upstream")
	}
	var first, second geo.Manifest
	if err := json.Unmarshal(before, &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(after, &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Files) != 2 || second.AuditRunID == 0 {
		t.Fatal("missing file inventory or audit reference")
	}
	for i, f := range second.Files {
		if f.SHA256 != first.Files[i].SHA256 || f.DownloadedAt != first.Files[i].DownloadedAt || f.Action != "not_modified" || !f.CheckedAt.After(first.Files[i].CheckedAt) {
			t.Fatal("conditional refresh lost provenance")
		}
	}
	runs, err := audit.History(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].Status != "success" {
		t.Fatal("SQLite history missing successful checks")
	}
	// A corrected object under the same monthly URL must be re-fetched, even
	// when this fixture happens to contain identical database bytes.
	release = "2"
	if err := u.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	thirdBytes, _ := os.ReadFile(filepath.Join(dir, "current.json"))
	var third geo.Manifest
	if err := json.Unmarshal(thirdBytes, &third); err != nil {
		t.Fatal(err)
	}
	if calls != 6 || third.Files[0].Action != "downloaded" || !third.Files[0].DownloadedAt.After(second.Files[0].DownloadedAt) {
		t.Fatal("same-month upstream correction not fetched")
	}
	// Corrupt local cache must not be reused on a subsequent 304.
	file := filepath.Join(dir, third.Version, "City.mmdb")
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)-1] ^= 1
	if err := os.WriteFile(file, b, 0644); err != nil {
		t.Fatal(err)
	}
	if err := u.Run(context.Background()); err != nil {
		t.Fatal("corrupt cache not repaired", err)
	}
}

func TestProviderSwitchUsesIndependentBuildDates(t *testing.T) {
	dir := t.TempDir()
	previous := geo.Manifest{Provider: "maxmind", Version: "old-snapshot"}
	for _, edition := range []string{"City", "ASN"} {
		fixture := "GeoIP2-City-Test.mmdb"
		if edition == "ASN" {
			fixture = "GeoLite2-ASN-Test.mmdb"
		}
		b, err := os.ReadFile(filepath.Join("..", "geo", "testdata", fixture))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, edition+".mmdb"), b, 0644); err != nil {
			t.Fatal(err)
		}
		previous.Files = append(previous.Files, inventory.File{Provider: "maxmind", Edition: strings.ToLower(edition), Path: edition + ".mmdb", BuildEpoch: ^uint(0)})
	}
	tracking := newTracking(t.TempDir(), dir, previous)
	if _, err := tracking.collect("dbip", false, false); err != nil {
		t.Fatal("different provider was treated as a rollback", err)
	}
	for key, record := range tracking.previous {
		record.Provider = "dbip"
		tracking.previous[key] = record
	}
	if _, err := tracking.collect("dbip", false, false); err == nil {
		t.Fatal("same-provider rollback no longer rejected")
	}
}

func TestAtomicJSON(t *testing.T) {
	p := filepath.Join(t.TempDir(), "current.json")
	for _, s := range []string{"first", "second"} {
		if err := AtomicJSON(p, map[string]string{"version": s}); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var v map[string]string
		if err := json.Unmarshal(b, &v); err != nil || v["version"] != s {
			t.Fatal("invalid publication")
		}
	}
}

func TestUpdaterLockExcludesConcurrentWriters(t *testing.T) {
	dir := t.TempDir()
	lock := flock.New(filepath.Join(dir, ".updater.lock"))
	defer lock.Close()
	locked, err := lock.TryLock()
	if err != nil || !locked {
		t.Fatal("cannot acquire test lock")
	}
	u := Updater{Dir: dir, Provider: "dbip", Client: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("download started while another writer holds lock")
		return nil, errors.New("unexpected download")
	})}}
	if err := u.Run(context.Background()); !errors.Is(err, ErrUpdateInProgress) {
		t.Fatalf("expected writer exclusion, got %v", err)
	}
}

func TestMissingFallbackCacheIsRedownloaded(t *testing.T) {
	u := Updater{Dir: t.TempDir()}
	calls := 0
	u.Client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		var b bytes.Buffer
		z := gzip.NewWriter(&b)
		_, _ = z.Write([]byte("fresh database"))
		_ = z.Close()
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(b.Bytes())), Header: make(http.Header)}, nil
	})}
	destination := t.TempDir()
	current := geo.Manifest{Version: "maxmind-missing", Provider: "maxmind", Hybrid: true, DBIPMonth: "2026-11"}
	if err := u.prepareFallback(context.Background(), destination, current, "2026-11"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("missing cache was not repaired")
	}
}

func TestTemporaryMaxMindConfiguration(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	u := Updater{AccountID: "123", LicenseKey: "test-token"}
	path, cleanup, err := u.maxmindConfig()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("secret file not private")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "AccountID 123\n") || !strings.Contains(string(b), "EditionIDs GeoLite2-City GeoLite2-ASN\n") {
		t.Fatal("bad generated configuration")
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("temporary credentials not removed")
	}
	u.LicenseKey = "bad\nEditionIDs bad"
	if _, _, err := u.maxmindConfig(); err == nil {
		t.Fatal("configuration injection accepted")
	}
}
func TestDownload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		z := gzip.NewWriter(w)
		_, _ = z.Write([]byte("database contents"))
		_ = z.Close()
	}))
	defer srv.Close()
	p := filepath.Join(t.TempDir(), "db")
	u := Updater{Client: srv.Client()}
	if err := u.download(context.Background(), srv.URL, p); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "database contents" {
		t.Fatal(string(b))
	}
}
func TestDownloadRejectsErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 404) }))
	defer srv.Close()
	u := Updater{Client: srv.Client()}
	if err := u.download(context.Background(), srv.URL, filepath.Join(t.TempDir(), "db")); err == nil {
		t.Fatal("accepted HTTP error")
	}
}

func TestIPInfoDownloadAndSecretSafeErrors(t *testing.T) {
	u := Updater{IPInfoToken: "private-test-token", Client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "ipinfo.io" || r.URL.Query().Get("token") != "private-test-token" {
			t.Fatal("wrong IPinfo request")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("database")), Header: make(http.Header)}, nil
	})}}
	path := filepath.Join(t.TempDir(), "IPinfo.mmdb")
	if err := u.downloadIPInfo(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "database" {
		t.Fatal("download changed")
	}
	u.Client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("upstream includes private-test-token")
	})}
	err := u.downloadIPInfo(context.Background(), filepath.Join(t.TempDir(), "IPinfo.mmdb"))
	if err == nil || strings.Contains(err.Error(), "private-test-token") {
		t.Fatal("missing error or leaked token")
	}
}
func TestFailedUpdatePreservesManifest(t *testing.T) {
	dir := t.TempDir()
	original := []byte(`{"version":"old","provider":"maxmind"}`)
	if err := os.WriteFile(filepath.Join(dir, "current.json"), original, 0644); err != nil {
		t.Fatal(err)
	}
	u := Updater{Dir: dir, Provider: "maxmind"}
	if u.Run(context.Background()) == nil {
		t.Fatal("missing credentials accepted")
	}
	b, _ := os.ReadFile(filepath.Join(dir, "current.json"))
	if string(b) != string(original) {
		t.Fatal("manifest modified on failure")
	}
	var status geo.UpdateStatus
	b, _ = os.ReadFile(filepath.Join(dir, "update-status.json"))
	if err := json.Unmarshal(b, &status); err != nil {
		t.Fatal(err)
	}
	if status.Failures != 1 || status.LastAttempt.IsZero() || !status.LastSuccess.IsZero() {
		t.Fatal("incorrect failure status")
	}
}
