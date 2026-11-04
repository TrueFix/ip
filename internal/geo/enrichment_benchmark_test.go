package geo

import (
	"testing"
	"time"
)

// These benchmarks measure enrichment only, not MMDB, HTTP, or network capacity.
// Fixed inputs and time make both revisions exercise the same work and DST rules.
var enrichmentInputs = []struct{ country, timezone string }{
	{"IN", "Asia/Kolkata"}, {"DE", "Europe/Berlin"},
	{"US", "America/New_York"}, {"BR", "America/Sao_Paulo"},
	{"AU", "Australia/Sydney"}, {"GB", "Europe/London"},
	{"ZA", "Africa/Johannesburg"}, {"JP", "Asia/Tokyo"},
}

func BenchmarkEnrichMixed(b *testing.B) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		input := enrichmentInputs[i%len(enrichmentInputs)]
		r := Response{CountryCode: &input.country, Timezone: Timezone{ID: &input.timezone}}
		enrich(&r, now)
		if r.Timezone.Offset == nil || r.Borders == nil {
			b.Fatal("incomplete enrichment")
		}
	}
}

func BenchmarkEnrichMixedParallel(b *testing.B) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			input := enrichmentInputs[i%len(enrichmentInputs)]
			r := Response{CountryCode: &input.country, Timezone: Timezone{ID: &input.timezone}}
			enrich(&r, now)
			if r.Timezone.Offset == nil || r.Borders == nil {
				b.Error("incomplete enrichment")
				return
			}
			i++
		}
	})
}
