// bench measures local MMDB lookups, not downloads or HTTP/network latency.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"net/netip"
	"os"
	"sort"
	"time"

	"github.com/TrueFix/myip/internal/buildinfo"
	"github.com/TrueFix/myip/internal/config"
	"github.com/TrueFix/myip/internal/geo"
)

type summary struct {
	Samples int     `json:"samples"`
	MeanUS  float64 `json:"mean_us"`
	P50US   float64 `json:"p50_us"`
	P95US   float64 `json:"p95_us"`
	P99US   float64 `json:"p99_us"`
}

func summarize(samples []time.Duration) summary {
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	var total float64
	for _, d := range samples {
		total += float64(d)
	}
	percentile := func(p float64) float64 {
		return float64(samples[max(0, int(math.Ceil(p*float64(len(samples))))-1)]) / float64(time.Microsecond)
	}
	return summary{Samples: len(samples), MeanUS: total / float64(len(samples)) / float64(time.Microsecond), P50US: percentile(.5), P95US: percentile(.95), P99US: percentile(.99)}
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	raw := flag.String("ip", "122.168.197.166", "public IPv4 or IPv6 address")
	n := flag.Int("n", 10000, "measured iterations (1..1000000)")
	warmup := flag.Int("warmup", 1000, "unmeasured warm-up iterations (0..100000)")
	all := flag.Bool("all-sources", true, "require and probe MaxMind, DB-IP and IPinfo; false measures the normal loaded-source path")
	version := flag.Bool("version", false, "print release version and exit")
	flag.Parse()
	if *version {
		fmt.Println(buildinfo.String())
		return nil
	}
	if *n < 1 || *n > 1000000 || *warmup < 0 || *warmup > 100000 {
		return errors.New("invalid iteration count")
	}
	ip, err := netip.ParseAddr(*raw)
	if err != nil || ip.Zone() != "" {
		return errors.New("invalid IP address")
	}
	ip = ip.Unmap()
	c, err := config.LoadRuntime()
	if err != nil {
		return err
	}
	s := geo.NewStore(c.DataDir, c.Provider, geo.Options{ISPFromASN: c.ISPFromASN})
	defer s.Close()
	if err := s.Reload(); err != nil {
		return fmt.Errorf("load snapshot: %w", err)
	}
	info, _ := s.IPInfoStats()
	city, asn := s.FallbackStats()
	if *all && (c.Provider != "maxmind" || !info || city == 0 || asn == 0) {
		return errors.New("three-source benchmark requires a hybrid snapshot with IPinfo Lite; configure IPINFO_ENABLED=true and IPINFO_TOKEN, run the updater, or use -all-sources=false for currently loaded sources")
	}
	lookup := s.Lookup
	mode := "normal_lookup"
	if *all {
		lookup = s.LookupAllSources
		mode = "forced_all_sources"
	}
	for range *warmup {
		if _, err := lookup(ip); err != nil {
			return err
		}
	}
	samples := map[string][]time.Duration{}
	for range *n {
		r, err := lookup(ip)
		if err != nil {
			return err
		}
		t := r.Timings
		samples["total"] = append(samples["total"], t.Total)
		if t.MaxMindCalled {
			samples["maxmind"] = append(samples["maxmind"], t.MaxMind)
		}
		if t.DBIPCalled {
			samples["dbip"] = append(samples["dbip"], t.DBIP)
		}
		if t.IPInfoCalled {
			samples["ipinfo"] = append(samples["ipinfo"], t.IPInfo)
		}
	}
	results := map[string]summary{}
	for source, data := range samples {
		results[source] = summarize(data)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(map[string]any{"ip": ip.String(), "mode": mode, "iterations": *n, "warmup": *warmup, "units": "microseconds", "note": "Warm in-process measurements include lookup instrumentation and enrichment; exclude database startup, HTTP serialization, network latency and updater downloads. Single-IP results are not a mixed-traffic capacity test.", "timings": results})
}
