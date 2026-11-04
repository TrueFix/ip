package geo

import (
	"encoding/json"
	"errors"
	"github.com/TrueFix/myip/internal/inventory"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func fixtureStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	snapshot := filepath.Join(dir, "test-version")
	if err := os.Mkdir(snapshot, 0755); err != nil {
		t.Fatal(err)
	}
	for src, dst := range map[string]string{"GeoIP2-City-Test.mmdb": "City.mmdb", "GeoLite2-ASN-Test.mmdb": "ASN.mmdb"} {
		b, err := os.ReadFile(filepath.Join("testdata", src))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(snapshot, dst), b, 0644); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := json.Marshal(Manifest{Version: "test-version", Provider: "dbip", UpdatedAt: time.Now()})
	if err := os.WriteFile(filepath.Join(dir, "current.json"), b, 0644); err != nil {
		t.Fatal(err)
	}
	s := NewStore(dir, "dbip")
	t.Cleanup(s.Close)
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	return s
}
func TestFixtureLookup(t *testing.T) {
	s := fixtureStore(t)
	r, err := s.Lookup(netip.MustParseAddr("81.2.69.142"))
	if err != nil {
		t.Fatal(err)
	}
	if r.CountryCode == nil || *r.CountryCode != "GB" || r.City == nil || *r.City != "London" {
		t.Fatalf("unexpected result: %+v", r)
	}
	if r.Latitude == nil || r.Timezone.Offset == nil {
		t.Fatal("coordinates or timezone missing")
	}
	if r.Connection.ISP != nil || r.Connection.Domain != nil {
		t.Fatal("fabricated network fields")
	}
	if !s.Ready() {
		t.Fatal("not ready")
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	if _, exists := fields["postal"]; exists {
		t.Fatal("removed postal field still serialized")
	}
}
func TestReloadPreservesWorkingReaders(t *testing.T) {
	s := fixtureStore(t)
	if err := os.WriteFile(filepath.Join(s.dir, "current.json"), []byte(`{"version":"../../etc","provider":"dbip"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if s.Reload() == nil {
		t.Fatal("invalid manifest accepted")
	}
	if _, err := s.Lookup(netip.MustParseAddr("81.2.69.142")); err != nil {
		t.Fatal("lost working reader", err)
	}
}

func TestPeriodicIntegrityFailureStopsServingAndCanRecover(t *testing.T) {
	s := fixtureStore(t)
	path := filepath.Join(s.dir, "test-version")
	m, _, _ := s.Stats()
	for _, spec := range inventory.Specs("dbip", false, false) {
		f, err := inventory.Inspect(path, spec)
		if err != nil {
			t.Fatal(err)
		}
		m.Files = append(m.Files, f)
	}
	b, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(s.dir, "current.json"), b, 0644); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(path, "City.mmdb")
	original, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	changed := append([]byte{}, original...)
	changed[len(changed)-1] ^= 1
	if err := os.WriteFile(file, changed, 0644); err != nil {
		t.Fatal(err)
	}
	s.integrityAt = time.Now().Add(-7 * time.Hour)
	if s.Reload() == nil || s.Ready() {
		t.Fatal("changed loaded files still ready")
	}
	if _, err := s.Lookup(netip.MustParseAddr("81.2.69.142")); !errors.Is(err, ErrUnavailable) {
		t.Fatal("tampered data still served")
	}
	if err := os.WriteFile(file, original, 0644); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(); err != nil || !s.Ready() {
		t.Fatal("restored files did not recover")
	}
}

func TestDBIPOnlyIgnoresFallbackFiles(t *testing.T) {
	s := fixtureStore(t)
	snapshot := filepath.Join(s.dir, "test-version")
	if err := os.Mkdir(filepath.Join(snapshot, "dbip"), 0755); err != nil {
		t.Fatal(err)
	}
	// Invalid leftover fallback files must never be opened in DB-IP-only mode.
	for _, name := range []string{"City.mmdb", "ASN.mmdb"} {
		if err := os.WriteFile(filepath.Join(snapshot, "dbip", name), []byte("invalid"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	if city, asn := s.FallbackStats(); city != 0 || asn != 0 {
		t.Fatal("DB-IP-only mode loaded fallback readers")
	}
	if _, err := s.Lookup(netip.MustParseAddr("81.2.69.142")); err != nil {
		t.Fatal(err)
	}
}
func TestConcurrentLookupReload(t *testing.T) {
	s := fixtureStore(t)
	// A distinct version forces reader replacement rather than a no-op reload.
	old := filepath.Join(s.dir, "test-version")
	newPath := filepath.Join(s.dir, "test-version-2")
	if err := os.Mkdir(newPath, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"City.mmdb", "ASN.mmdb"} {
		b, err := os.ReadFile(filepath.Join(old, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(newPath, name), b, 0644); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				_, err := s.Lookup(netip.MustParseAddr("81.2.69.142"))
				if err != nil {
					t.Error(err)
				}
			}
		})
	}
	for i := range 10 {
		version := "test-version"
		if i%2 == 0 {
			version = "test-version-2"
		}
		b, _ := json.Marshal(Manifest{Version: version, Provider: "dbip", UpdatedAt: time.Now()})
		if err := os.WriteFile(filepath.Join(s.dir, "current.json"), b, 0644); err != nil {
			t.Fatal(err)
		}
		if err := s.Reload(); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}

func TestLiveSnapshot(t *testing.T) {
	dir := os.Getenv("MYIP_TEST_DATA_DIR")
	if dir == "" {
		t.Skip("set MYIP_TEST_DATA_DIR to test a locally downloaded snapshot")
	}
	provider := os.Getenv("MYIP_TEST_PROVIDER")
	if provider == "" {
		provider = "maxmind"
	}
	s := NewStore(dir, provider)
	defer s.Close()
	if err := s.Reload(); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"8.8.8.8", "2401:4900:1ca2:48ff:5516:e857:6908:1227", "122.168.197.166", "1.1.1.1"} {
		r, err := s.Lookup(netip.MustParseAddr(raw))
		if err != nil {
			t.Fatal(err)
		}
		if !r.Success || r.IP != raw || r.CountryCode == nil || r.Connection.ASN == nil {
			t.Fatalf("incomplete record for %s", raw)
		}
		if provider == "maxmind" && (raw == "2401:4900:1ca2:48ff:5516:e857:6908:1227" || raw == "122.168.197.166") && r.Timezone.Offset == nil {
			t.Fatalf("missing timezone for %s", raw)
		}
		if provider == "maxmind" && s.manifest.Hybrid && raw == "122.168.197.166" {
			if r.City == nil || *r.City != "Gwalior" || r.RegionCode == nil || *r.RegionCode != "MP" || r.Latitude == nil || *r.Latitude != 26.2298 || r.Timezone.ID == nil || *r.Timezone.ID != "Asia/Kolkata" {
				t.Fatalf("hybrid location/timezone regression: %+v", r)
			}
		}
	}
}
func TestEnrich(t *testing.T) {
	r := Response{CountryCode: str("IN"), Timezone: Timezone{ID: str("Asia/Kolkata")}}
	enrich(&r, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if r.CallingCode == nil || *r.CallingCode != "91" || r.Flag.Emoji == nil || *r.Flag.Emoji != "🇮🇳" || r.Capital == nil || *r.Capital != "New Delhi" {
		t.Fatalf("bad enrichment: %+v", r)
	}
	if r.Timezone.UTC == nil || *r.Timezone.UTC != "+05:30" {
		t.Fatal("wrong offset")
	}
	for _, month := range []time.Month{time.January, time.July} {
		r := Response{Timezone: Timezone{ID: str("America/New_York")}}
		enrich(&r, time.Date(2026, month, 1, 0, 0, 0, 0, time.UTC))
		if r.Timezone.IsDST == nil || *r.Timezone.IsDST != (month == time.July) {
			t.Fatal("DST not calculated dynamically")
		}
	}
}
