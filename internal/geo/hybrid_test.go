package geo

import (
	"testing"
)

func location(country, city, zone string, lat, lon float64) cityRecord {
	var c cityRecord
	c.Country.Code = country
	c.City.Names = map[string]string{"en": city}
	c.Location.Latitude = &lat
	c.Location.Longitude = &lon
	c.Location.Timezone = zone
	return c
}

func TestHybridLocationSelection(t *testing.T) {
	primary := location("IN", "", "Asia/Kolkata", 21.9974, 79.0011)
	primary.Continent.Code = "unrelated-primary-continent"
	fallback := location("IN", "Gwalior", "", 26.2298, 78.1734)
	if !useFallback(primary, fallback) {
		t.Fatal("missing-city fallback not selected")
	}
	selected := selectLocation(primary, fallback)
	if selected.City.Names["en"] != "Gwalior" || *selected.Location.Latitude != 26.2298 || *selected.Location.Longitude != 78.1734 {
		t.Fatal("location record was mixed")
	}
	if selected.Continent.Code != "" {
		t.Fatal("unrelated primary continent retained")
	}
	if selected.Location.Timezone != "Asia/Kolkata" {
		t.Fatal("compatible sole-country timezone lost")
	}
	primary.City.Names["en"] = "Bhopal"
	if useFallback(primary, fallback) {
		t.Fatal("fallback replaced a primary city")
	}
}

func TestHybridTimezoneCompatibility(t *testing.T) {
	for _, tc := range []struct{ name, country, zone, fallbackCountry, fallbackZone, want string }{
		{"single zone", "IN", "Asia/Kolkata", "IN", "", "Asia/Kolkata"},
		{"multiple zones", "US", "America/Chicago", "US", "", ""},
		{"country conflict", "IN", "Asia/Kolkata", "AU", "", ""},
		{"unknown country", "ZZ", "Etc/UTC", "ZZ", "", ""},
		{"incorrect primary zone", "IN", "America/Chicago", "IN", "", ""},
		{"fallback zone wins", "US", "America/Chicago", "US", "America/New_York", "America/New_York"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := location(tc.country, "", tc.zone, 0, 0)
			f := location(tc.fallbackCountry, "fallback", tc.fallbackZone, 1, 1)
			if got := selectLocation(p, f).Location.Timezone; got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHybridDoesNotReplaceBetterPartialRecord(t *testing.T) {
	primary := location("IN", "", "Asia/Kolkata", 21, 79)
	fallback := location("AU", "", "", 0, 0)
	if useFallback(primary, fallback) {
		t.Fatal("replaced known country with another country-only result")
	}
	primary.Country.Code = ""
	if !useFallback(primary, fallback) {
		t.Fatal("did not recover missing country")
	}
}
