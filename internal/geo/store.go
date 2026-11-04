package geo

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/TrueFix/myip/internal/inventory"
	"github.com/oschwald/maxminddb-golang"
)

var ErrUnavailable = errors.New("databases unavailable")
var ErrNotFound = errors.New("IP not found")

type Manifest struct {
	Version    string           `json:"version"`
	Provider   string           `json:"provider"`
	UpdatedAt  time.Time        `json:"updated_at"`
	Hybrid     bool             `json:"hybrid,omitempty"`
	DBIPMonth  string           `json:"dbip_month,omitempty"`
	IPInfo     bool             `json:"ipinfo,omitempty"`
	AuditRunID int64            `json:"audit_run_id,omitempty"`
	Files      []inventory.File `json:"files,omitempty"`
}
type UpdateStatus struct {
	LastAttempt time.Time        `json:"last_attempt"`
	LastSuccess time.Time        `json:"last_success"`
	Failures    uint64           `json:"failures"`
	Snapshot    string           `json:"snapshot,omitempty"`
	AuditRunID  int64            `json:"audit_run_id,omitempty"`
	Files       []inventory.File `json:"files,omitempty"`
}
type Store struct {
	reloadMu                  sync.Mutex
	mu                        sync.RWMutex
	dir, provider             string
	manifest                  Manifest
	city, asn                 *maxminddb.Reader
	fallbackCity, fallbackASN *maxminddb.Reader
	ipinfo                    *maxminddb.Reader
	ispFromASN                bool
	integrityAt               time.Time
	integrityFailed           bool
}

type Options struct{ ISPFromASN bool }

func NewStore(dir, provider string, options ...Options) *Store {
	s := &Store{dir: dir, provider: provider}
	if len(options) > 0 && provider == "maxmind" {
		s.ispFromASN = options[0].ISPFromASN
	}
	return s
}

func OpenPair(dir, provider string) (*maxminddb.Reader, *maxminddb.Reader, error) {
	c, err := maxminddb.Open(filepath.Join(dir, "City.mmdb"))
	if err != nil {
		return nil, nil, err
	}
	a, err := maxminddb.Open(filepath.Join(dir, "ASN.mmdb"))
	if err != nil {
		c.Close()
		return nil, nil, err
	}
	ok := strings.Contains(strings.ToLower(c.Metadata.DatabaseType), "city") && strings.Contains(strings.ToLower(a.Metadata.DatabaseType), "asn")
	if provider == "maxmind" {
		ok = c.Metadata.DatabaseType == "GeoLite2-City" && a.Metadata.DatabaseType == "GeoLite2-ASN"
	}
	if !ok {
		c.Close()
		a.Close()
		return nil, nil, errors.New("unexpected database types")
	}
	if err := c.Verify(); err != nil {
		c.Close()
		a.Close()
		return nil, nil, fmt.Errorf("city validation: %w", err)
	}
	if err := a.Verify(); err != nil {
		c.Close()
		a.Close()
		return nil, nil, fmt.Errorf("ASN validation: %w", err)
	}
	return c, a, nil
}

func (s *Store) Reload() error {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	b, err := os.ReadFile(filepath.Join(s.dir, "current.json"))
	if err != nil {
		return err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	if m.Provider != s.provider || !ValidVersion(m.Version) {
		return errors.New("invalid database manifest")
	}
	s.mu.RLock()
	same := s.city != nil && s.manifest.Version == m.Version
	loaded := s.manifest
	lastVerified := s.integrityAt
	failed := s.integrityFailed
	s.mu.RUnlock()
	if same {
		if len(loaded.Files) > 0 && (failed || time.Since(lastVerified) >= 6*time.Hour) {
			err := inventory.Verify(filepath.Join(s.dir, loaded.Version), loaded.Files, inventory.Specs(loaded.Provider, loaded.Hybrid, loaded.IPInfo))
			s.mu.Lock()
			s.integrityFailed = err != nil
			if err == nil {
				s.integrityAt = time.Now().UTC()
			}
			s.mu.Unlock()
			return err
		}
		return nil
	}
	if len(m.Files) > 0 {
		if err := inventory.Verify(filepath.Join(s.dir, m.Version), m.Files, inventory.Specs(m.Provider, m.Hybrid, m.IPInfo)); err != nil {
			return err
		}
	}
	c, a, err := OpenPair(filepath.Join(s.dir, m.Version), s.provider)
	if err != nil {
		return err
	}
	var fc, fa *maxminddb.Reader
	if s.provider == "maxmind" && m.Hybrid {
		fc, fa, err = OpenPair(filepath.Join(s.dir, m.Version, "dbip"), "dbip")
		if err != nil {
			c.Close()
			a.Close()
			return fmt.Errorf("hybrid fallback validation: %w", err)
		}
	}
	var info *maxminddb.Reader
	if s.provider == "maxmind" && m.IPInfo {
		info, err = OpenIPInfo(filepath.Join(s.dir, m.Version, "IPinfo.mmdb"))
		if err != nil {
			c.Close()
			a.Close()
			if fc != nil {
				fc.Close()
				fa.Close()
			}
			return fmt.Errorf("IPinfo validation: %w", err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	oldC, oldA := s.city, s.asn
	oldInfo := s.ipinfo
	oldFC, oldFA := s.fallbackCity, s.fallbackASN
	s.city, s.asn, s.manifest = c, a, m
	s.fallbackCity, s.fallbackASN = fc, fa
	s.ipinfo = info
	s.integrityFailed = false
	s.integrityAt = time.Time{}
	if len(m.Files) > 0 {
		s.integrityAt = time.Now().UTC()
	}
	if oldC != nil {
		oldC.Close()
		oldA.Close()
	}
	if oldFC != nil {
		oldFC.Close()
		oldFA.Close()
	}
	if oldInfo != nil {
		oldInfo.Close()
	}
	return nil
}
func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.city != nil {
		s.city.Close()
		s.asn.Close()
		s.city = nil
		s.asn = nil
	}
	if s.fallbackCity != nil {
		s.fallbackCity.Close()
		s.fallbackASN.Close()
		s.fallbackCity = nil
		s.fallbackASN = nil
	}
	if s.ipinfo != nil {
		s.ipinfo.Close()
		s.ipinfo = nil
	}
}

func (s *Store) IPInfoStats() (bool, uint) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.ipinfo == nil {
		return false, 0
	}
	return true, s.ipinfo.Metadata.BuildEpoch
}
func (s *Store) FallbackStats() (uint, uint) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.fallbackCity == nil {
		return 0, 0
	}
	return s.fallbackCity.Metadata.BuildEpoch, s.fallbackASN.Metadata.BuildEpoch
}
func (s *Store) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.city != nil && !s.integrityFailed
}
func (s *Store) IntegrityStats() (bool, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.city != nil && len(s.manifest.Files) > 0 && !s.integrityFailed, s.integrityAt
}
func (s *Store) Stats() (Manifest, uint, uint) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.city == nil {
		return Manifest{}, 0, 0
	}
	return s.manifest, s.city.Metadata.BuildEpoch, s.asn.Metadata.BuildEpoch
}

// Pointer coordinates distinguish missing data from valid zero coordinates.
type cityRecord struct {
	Continent struct {
		Code  string            `maxminddb:"code"`
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"continent"`
	Country struct {
		Code  string            `maxminddb:"iso_code"`
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	Subdivisions []struct {
		Code  string            `maxminddb:"iso_code"`
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"subdivisions"`
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Location struct {
		Latitude  *float64 `maxminddb:"latitude"`
		Longitude *float64 `maxminddb:"longitude"`
		Timezone  string   `maxminddb:"time_zone"`
	} `maxminddb:"location"`
}
type asnRecord struct {
	ASN uint   `maxminddb:"autonomous_system_number"`
	Org string `maxminddb:"autonomous_system_organization"`
}

func (s *Store) Lookup(ip netip.Addr) (Response, error) { return s.lookup(ip, false) }

// LookupAllSources is a diagnostic/benchmark path. It probes all loaded sources
// even when fallback is unnecessary, without changing source selection policy.
func (s *Store) LookupAllSources(ip netip.Addr) (Response, error) { return s.lookup(ip, true) }

func (s *Store) lookup(ip netip.Addr, probeAll bool) (response Response, lookupErr error) {
	start := time.Now()
	timing := LookupTimings{}
	defer func() { timing.Total = time.Since(start); response.Timings = timing }()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.city == nil || s.integrityFailed {
		return Response{}, ErrUnavailable
	}
	var c cityRecord
	var a asnRecord
	nip := net.IP(ip.AsSlice())
	lookup := func(db *maxminddb.Reader, record any, source string) (bool, error) {
		start := time.Now()
		_, found, err := db.LookupNetwork(nip, record)
		elapsed := time.Since(start)
		switch source {
		case "maxmind":
			timing.MaxMind += elapsed
			timing.MaxMindCalled = true
		case "dbip":
			timing.DBIP += elapsed
			timing.DBIPCalled = true
		case "ipinfo":
			timing.IPInfo += elapsed
			timing.IPInfoCalled = true
		}
		return found, err
	}
	foundCity, err := lookup(s.city, &c, s.provider)
	if err != nil {
		return Response{}, err
	}
	foundASN, err := lookup(s.asn, &a, s.provider)
	if err != nil {
		return Response{}, err
	}
	if s.fallbackCity != nil && (probeAll || c.City.Names["en"] == "") {
		var fallback cityRecord
		found, err := lookup(s.fallbackCity, &fallback, "dbip")
		if err != nil {
			return Response{}, err
		}
		if found && useFallback(c, fallback) {
			c = selectLocation(c, fallback)
			foundCity = true
		}
	}
	if s.fallbackASN != nil && (probeAll || a.ASN == 0) {
		var fallback asnRecord
		found, err := lookup(s.fallbackASN, &fallback, "dbip")
		if err != nil {
			return Response{}, err
		}
		if a.ASN == 0 && found && fallback.ASN != 0 {
			a = fallback
			foundASN = true
		}
	}
	var info ipinfoRecord
	foundInfo := false
	if s.ipinfo != nil {
		var err error
		foundInfo, err = lookup(s.ipinfo, &info, "ipinfo")
		if err != nil {
			return Response{}, err
		}
	}
	if !foundCity && !foundASN && (!foundInfo || asnNumber(info.ASN) == 0) {
		return Response{}, ErrNotFound
	}
	r := Response{IP: ip.String(), Success: true, Type: "IPv6", Readme: "/docs"}
	if ip.Is4() {
		r.Type = "IPv4"
	}
	r.Continent = str(c.Continent.Names["en"])
	r.ContinentCode = str(c.Continent.Code)
	r.Country = str(c.Country.Names["en"])
	r.CountryCode = str(c.Country.Code)
	r.City = str(c.City.Names["en"])
	if len(c.Subdivisions) > 0 {
		r.Region = str(c.Subdivisions[0].Names["en"])
		r.RegionCode = str(c.Subdivisions[0].Code)
	}
	r.Latitude = c.Location.Latitude
	r.Longitude = c.Location.Longitude
	r.Timezone.ID = str(c.Location.Timezone)
	if a.ASN != 0 {
		r.Connection.ASN = &a.ASN
	}
	r.Connection.Org = str(a.Org)
	if foundInfo {
		r.IPInfoUsed = enrichNetwork(&r, info, s.ispFromASN)
	}
	enrich(&r, time.Now())
	return r, nil
}

// Prefer a full fallback location only when it provides a city (or a missing
// country). Never stitch fallback city names onto unrelated primary coordinates.
func useFallback(primary, fallback cityRecord) bool {
	return primary.City.Names["en"] == "" && (fallback.City.Names["en"] != "" || (primary.Country.Code == "" && fallback.Country.Code != ""))
}

func selectLocation(primary, fallback cityRecord) cityRecord {
	if fallback.Location.Timezone == "" && primary.Country.Code != "" && primary.Country.Code == fallback.Country.Code {
		// Matching countries alone is insufficient for multi-timezone countries.
		// Preserve only an IANA-listed sole timezone, never infer from longitude.
		if zone := singleTimezones[fallback.Country.Code]; zone != "" && zone == primary.Location.Timezone {
			fallback.Location.Timezone = zone
		}
	}
	return fallback
}
