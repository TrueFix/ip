package geo

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TrueFix/myip/internal/assets"
)

func TestPrecomputedCountryEnrichment(t *testing.T) {
	for code, country := range countries {
		var borders []string
		for _, border := range country.Borders {
			for _, other := range countries {
				if other.CCA3 == border {
					borders = append(borders, other.CCA2)
					break
				}
			}
		}
		sort.Strings(borders)
		r := Response{CountryCode: str(code)}
		enrich(&r, time.Now())
		if r.Borders == nil || *r.Borders != strings.Join(borders, ",") {
			t.Fatalf("%s: incorrect borders: %v", code, r.Borders)
		}
		path := "/flags/" + strings.ToLower(code) + ".svg"
		_, err := assets.Files.ReadFile(strings.TrimPrefix(path, "/"))
		if (r.Flag.Image != nil) != (err == nil) || (r.Flag.Image != nil && *r.Flag.Image != path) {
			t.Fatalf("%s: incorrect flag availability/path", code)
		}
		// Callers must not be able to mutate shared precomputed strings.
		*r.Borders = "changed"
		if r.Flag.Image != nil {
			*r.Flag.Image = "changed"
		}
		next := Response{CountryCode: str(code)}
		enrich(&next, time.Now())
		if *next.Borders != strings.Join(borders, ",") || (next.Flag.Image != nil && *next.Flag.Image != path) {
			t.Fatalf("%s: response mutation changed shared metadata", code)
		}
	}
}

func TestEnrichmentUnknownAndMissingCountry(t *testing.T) {
	for _, code := range []*string{nil, str("ZZ"), str("us"), str("invalid")} {
		r := Response{CountryCode: code}
		enrich(&r, time.Now())
		if r.Borders != nil || r.Flag.Image != nil || r.Capital != nil {
			t.Fatal("unknown or absent country gained metadata")
		}
		if code != nil && *code == "ZZ" && (r.Flag.Emoji == nil || r.Flag.Unicode == nil) {
			t.Fatal("uppercase unknown country lost existing emoji behavior")
		}
	}
}

func TestCachedTimezoneKeepsDynamicTransitions(t *testing.T) {
	for _, zone := range []string{"America/New_York", "US/Eastern", "Australia/Sydney", "Asia/Kolkata", "UTC"} {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		for _, now := range []time.Time{
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 3, 8, 6, 59, 59, 0, time.UTC),
			time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC),
		} {
			r := Response{Timezone: Timezone{ID: str(zone)}}
			enrich(&r, now)
			want := now.In(loc)
			abbr, offset := want.Zone()
			if r.Timezone.Offset == nil || *r.Timezone.Offset != offset || r.Timezone.Abbr == nil || *r.Timezone.Abbr != abbr || r.Timezone.IsDST == nil || *r.Timezone.IsDST != want.IsDST() {
				t.Fatalf("%s at %s: cached location changed timezone rules", zone, now)
			}
		}
	}
}

func TestLocationCacheBoundsAndNegativeEntries(t *testing.T) {
	var cache locationCache
	if cache.load("UTC") != time.UTC || cache.load("UTC") != time.UTC {
		t.Fatal("valid location not cached")
	}
	if cache.load("Invalid/Timezone") != nil {
		t.Fatal("invalid timezone accepted")
	}
	if loc, ok := cache.entries["Invalid/Timezone"]; !ok || loc != nil {
		t.Fatal("failed load not negatively cached")
	}
	for i := 0; i < timezoneCacheSize*2; i++ {
		// NUL-containing names are rejected without searching timezone files.
		if cache.load(fmt.Sprintf("invalid\x00%d", i)) != nil {
			t.Fatal("invalid timezone accepted")
		}
		if len(cache.entries) > timezoneCacheSize {
			t.Fatal("timezone cache exceeded capacity")
		}
	}
	if len(cache.entries) != timezoneCacheSize {
		t.Fatal("cache did not retain its bounded working set")
	}
	before := cache.next
	if cache.load(strings.Repeat("x", maxTimezoneNameBytes+1)) != nil || cache.next != before {
		t.Fatal("oversized name was loaded or retained")
	}
	if cache.load("UTC") != time.UTC {
		t.Fatal("evicted valid location could not reload")
	}
	r := Response{Timezone: Timezone{ID: str("Invalid/Timezone")}}
	enrich(&r, time.Now())
	if r.Timezone.Offset != nil || r.Timezone.Abbr != nil || r.Timezone.IsDST != nil || r.Timezone.UTC != nil {
		t.Fatal("invalid timezone gained enrichment")
	}
}

func TestLocationCacheConcurrent(t *testing.T) {
	var cache locationCache
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if cache.load("America/New_York") == nil || cache.load("Invalid/Timezone") != nil {
					t.Error("unexpected concurrent cache result")
					return
				}
			}
		}()
	}
	wg.Wait()
}
