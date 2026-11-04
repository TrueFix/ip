package geo

import (
	"testing"
	"time"
)

func TestRegionCodeLookup(t *testing.T) {
	for _, tc := range []struct{ country, name, want string }{
		{"IN", "Madhya Pradesh", "MP"},
		{"in", "  madhya   pradesh  ", "MP"},
		{"IN", "Maharashtra", "MH"},
		{"BR", "Sao Paulo", "SP"},
		{"US", "California", "CA"},
		{"AU", "New South Wales", "NSW"},
		{"GB", "England", "ENG"},
		{"AT", "Salzburg", "5"},
		{"US", "Madhya Pradesh", ""},
		{"IN", "Unknown State", ""},
		{"", "Madhya Pradesh", ""},
	} {
		t.Run(tc.country+"/"+tc.name, func(t *testing.T) {
			got := regionCodeFor(tc.country, tc.name)
			if tc.want == "" {
				if got != nil {
					t.Fatalf("invented code %q", *got)
				}
				return
			}
			if got == nil || *got != tc.want {
				t.Fatalf("got %v, want %s", got, tc.want)
			}
		})
	}
}

func TestSubdivisionAmbiguityAndAdministrativeLevel(t *testing.T) {
	index := indexSubdivisions([]subdivision{
		{Code: "XY-A", Name: "Duplicate", Parent: "XY"},
		{Code: "XY-B", Name: "duplicate", Parent: "XY"},
		{Code: "XY-A", Name: "Duplicate", Parent: "XY"},
		{Code: "XY-C", Name: "District", Parent: "XY-A"},
		{Code: "ZZ-A", Name: "Other", Parent: "XY"},
	})
	if index[subdivisionKey{"XY", "duplicate"}] != "" {
		t.Fatal("ambiguous name resolved arbitrarily")
	}
	if _, ok := index[subdivisionKey{"XY", "district"}]; ok {
		t.Fatal("district treated as state")
	}
	if _, ok := index[subdivisionKey{"XY", "other"}]; ok {
		t.Fatal("mismatched country prefix accepted")
	}
}

func TestRegionEnrichmentPreservesProviderCode(t *testing.T) {
	for _, existing := range []*string{nil, str("provider-code")} {
		r := Response{CountryCode: str("IN"), Region: str("Madhya Pradesh"), RegionCode: existing}
		enrich(&r, time.Now())
		want := "MP"
		if existing != nil {
			want = *existing
		}
		if r.RegionCode == nil || *r.RegionCode != want {
			t.Fatal("missing enrichment or overwritten provider code")
		}
		if *r.Region != "Madhya Pradesh" {
			t.Fatal("region name changed")
		}
	}
}
