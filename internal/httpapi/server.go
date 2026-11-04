package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/TrueFix/myip/internal/assets"
	"github.com/TrueFix/myip/internal/buildinfo"
	"github.com/TrueFix/myip/internal/geo"
	"github.com/TrueFix/myip/internal/inventory"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Lookup interface {
	Lookup(netip.Addr) (geo.Response, error)
	Ready() bool
}
type Server struct {
	Config         Config
	Store          Lookup
	limiter        *Limiter
	requests       *prometheus.CounterVec
	duration       *prometheus.HistogramVec
	lookups        *prometheus.CounterVec
	sourceDuration *prometheus.HistogramVec
	Registry       *prometheus.Registry
}

func New(c Config, store Lookup) *Server {
	s := &Server{Config: c, Store: store, limiter: NewLimiter(c.RPM, c.Rate, c.Burst, c.MaxClients), Registry: prometheus.NewRegistry()}
	s.requests = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "myip_http_requests_total", Help: "HTTP requests by bounded route, method and status."}, []string{"route", "method", "status"})
	s.duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "myip_http_request_duration_seconds", Help: "HTTP response latency.", Buckets: prometheus.DefBuckets}, []string{"route"})
	s.lookups = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "myip_lookups_total", Help: "IP lookup outcomes."}, []string{"result"})
	s.sourceDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "myip_geoip_lookup_duration_seconds", Help: "Local database lookup duration by source; total includes enrichment and lock wait, excludes HTTP/network.", Buckets: []float64{.000001, .000005, .00001, .000025, .00005, .0001, .00025, .0005, .001, .005, .01, .1}}, []string{"source"})
	s.Registry.MustRegister(s.sourceDuration)
	s.Registry.MustRegister(s.requests, s.duration, s.lookups, prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	info := buildinfo.Current()
	buildGauge := prometheus.NewGauge(prometheus.GaugeOpts{Name: "myip_build_info", Help: "Release version metadata.", ConstLabels: prometheus.Labels{"version": info.Version, "revision": info.Revision, "go_version": info.GoVersion}})
	buildGauge.Set(1)
	s.Registry.MustRegister(buildGauge)
	s.Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "myip_rate_limit_clients", Help: "Current in-memory client buckets (IPv6 aggregated by /64)."}, func() float64 { return float64(s.limiter.Stats().Clients) }))
	for _, reason := range []string{"rpm", "rps", "capacity"} {
		s.Registry.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Name: "myip_rate_limit_rejections_total", Help: "Process-local rate-limit rejections; no IP labels.", ConstLabels: prometheus.Labels{"reason": reason}}, func() float64 {
			stats := s.limiter.Stats()
			switch reason {
			case "rpm":
				return float64(stats.DeniedRPM)
			case "rps":
				return float64(stats.DeniedRPS)
			default:
				return float64(stats.DeniedCapacity)
			}
		}))
	}
	s.Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "myip_ready", Help: "Whether databases are loaded."}, func() float64 {
		if store.Ready() {
			return 1
		}
		return 0
	}))
	if db, ok := store.(*geo.Store); ok {
		for _, edition := range []string{"city", "asn"} {
			s.Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "myip_database_build_timestamp_seconds", Help: "Loaded database build timestamp.", ConstLabels: prometheus.Labels{"edition": edition}}, func() float64 {
				_, c, a := db.Stats()
				if edition == "city" {
					return float64(c)
				}
				return float64(a)
			}))
		}
		s.Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "myip_database_loaded_update_timestamp_seconds", Help: "Publication timestamp of the loaded snapshot."}, func() float64 {
			m, _, _ := db.Stats()
			if m.UpdatedAt.IsZero() {
				return 0
			}
			return float64(m.UpdatedAt.Unix())
		}))
	}
	if db, ok := store.(*geo.Store); ok && c.Provider == "maxmind" {
		s.Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "myip_ipinfo_database_build_timestamp_seconds", Help: "IPinfo Lite build timestamp; zero when not loaded."}, func() float64 { _, build := db.IPInfoStats(); return float64(build) }))
		for _, edition := range []string{"city", "asn"} {
			s.Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "myip_fallback_database_build_timestamp_seconds", Help: "Loaded DB-IP fallback build timestamp; zero until available.", ConstLabels: prometheus.Labels{"edition": edition}}, func() float64 {
				city, asn := db.FallbackStats()
				if edition == "city" {
					return float64(city)
				}
				return float64(asn)
			}))
		}
	}
	status := func() geo.UpdateStatus {
		var v geo.UpdateStatus
		b, err := os.ReadFile(filepath.Join(c.DataDir, "update-status.json"))
		if err == nil {
			_ = json.Unmarshal(b, &v)
		}
		return v
	}
	s.Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "myip_database_update_last_success_timestamp_seconds", Help: "Last successful updater check (zero if unknown)."}, func() float64 {
		v := status()
		if v.LastSuccess.IsZero() {
			return 0
		}
		return float64(v.LastSuccess.Unix())
	}))
	s.Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "myip_database_update_failures", Help: "Cumulative failed update attempts recorded by updater."}, func() float64 { return float64(status().Failures) }))
	for _, spec := range inventory.Specs(c.Provider, c.Provider == "maxmind", c.Provider == "maxmind") {
		for _, field := range []string{"checked", "downloaded", "verified"} {
			s.Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "myip_database_" + field + "_timestamp_seconds", Help: "Per-file metadata timestamp; zero means unavailable or unknown.", ConstLabels: prometheus.Labels{"provider": spec.Provider, "edition": spec.Edition}}, func() float64 {
				for _, file := range status().Files {
					if file.Provider == spec.Provider && file.Edition == spec.Edition {
						var timestamp time.Time
						switch field {
						case "checked":
							timestamp = file.CheckedAt
						case "downloaded":
							timestamp = file.DownloadedAt
						case "verified":
							timestamp = file.VerifiedAt
						}
						if !timestamp.IsZero() {
							return float64(timestamp.Unix())
						}
					}
				}
				return 0
			}))
		}
	}
	if db, ok := store.(*geo.Store); ok {
		s.Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "myip_database_integrity_ok", Help: "Loaded snapshot has verified SHA-256 inventory; legacy/unverified/failed snapshots report zero."}, func() float64 {
			verified, _ := db.IntegrityStats()
			if verified {
				return 1
			}
			return 0
		}))
		s.Registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "myip_database_snapshot_matches_published", Help: "API loaded snapshot matches updater's published snapshot and audit ID."}, func() float64 {
			m, _, _ := db.Stats()
			published := status()
			if m.Version != "" && m.Version == published.Snapshot && m.AuditRunID == published.AuditRunID {
				return 1
			}
			return 0
		}))
	}
	return s
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (w *recorder) WriteHeader(n int) {
	if w.status == 0 {
		w.status = n
		w.ResponseWriter.WriteHeader(n)
	}
}
func (w *recorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
func (w *recorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func route(path string) string {
	switch {
	case path == "/":
		return "/"
	case path == "/healthz":
		return "/healthz"
	case path == "/metrics":
		return "/metrics"
	case path == "/docs":
		return "/docs"
	case path == "/country-data.json":
		return "/country-data.json"
	case strings.HasPrefix(path, "/flags/"):
		return "/flags/{country}"
	default:
		return "/{ip}"
	}
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, pattern := range []string{"/{$}", "/{ip}", "/healthz", "/metrics", "/docs", "/flags/{country}", "/country-data.json"} {
		mux.HandleFunc(pattern, s.serve)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "Not found") })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &recorder{ResponseWriter: w}
		defer func() {
			if recover() != nil {
				slog.Error("request handler panic")
				fail(rw, 500, "Internal server error")
			}
			status := rw.status
			if status == 0 {
				status = 200
			}
			method := r.Method
			switch method {
			case "GET", "HEAD", "OPTIONS", "POST", "PUT", "PATCH", "DELETE":
			default:
				method = "OTHER"
			}
			s.requests.WithLabelValues(route(r.URL.Path), method, strconv.Itoa(status)).Inc()
			s.duration.WithLabelValues(route(r.URL.Path)).Observe(time.Since(start).Seconds())
		}()
		rw.Header().Set("X-Content-Type-Options", "nosniff")
		rw.Header().Set("Referrer-Policy", "no-referrer")
		rw.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(rw, r)
	})
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}{false, message})
}
func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
		w.Header().Set("Allow", "GET, HEAD, OPTIONS")
		fail(w, 405, "Method not allowed")
		return
	}
	if s.Config.CORSOrigin != "" {
		w.Header().Set("Access-Control-Allow-Origin", s.Config.CORSOrigin)
		w.Header().Set("Access-Control-Expose-Headers", "Server-Timing, Link")
	}
	if r.Method == "OPTIONS" {
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		w.WriteHeader(204)
		return
	}
	if r.URL.Path == "/healthz" {
		if !s.Store.Ready() {
			fail(w, 503, "Databases unavailable")
			return
		}
		writeJSON(w, 200, map[string]bool{"success": true})
		return
	}
	// Metrics are intentionally only served on a separate, unpublished listener.
	if r.URL.Path == "/metrics" {
		fail(w, 404, "Not found")
		return
	}
	client, err := ClientIP(r, s.Config.TrustedProxies, s.Config.ProxyHeader)
	if err != nil {
		fail(w, 400, "Invalid client IP forwarding header")
		return
	}
	if allowed, retry := s.limiter.AllowWithRetry(client, time.Now()); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(retry.Seconds())))))
		fail(w, 429, "Rate limit exceeded")
		return
	}
	origin, err := requestOrigin(r, s.Config)
	if err != nil {
		fail(w, 400, "Invalid or disallowed request origin")
		return
	}
	if r.URL.Path == "/docs" {
		s.docs(w)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/flags/") {
		name := strings.TrimPrefix(r.URL.Path, "/flags/")
		if len(name) != 6 || name[2:] != ".svg" || name[0] < 'a' || name[0] > 'z' || name[1] < 'a' || name[1] > 'z' {
			fail(w, 404, "Not found")
			return
		}
		b, err := assets.Files.ReadFile("flags/" + name)
		if err != nil {
			fail(w, 404, "Not found")
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.WriteHeader(200)
		_, _ = w.Write(b)
		return
	}
	if r.URL.Path == "/country-data.json" {
		b, _ := assets.Files.ReadFile("countries.json")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write(b)
		return
	}
	if strings.Contains(strings.TrimPrefix(r.URL.Path, "/"), "/") || len(r.URL.Path) > 64 {
		fail(w, 404, "Not found")
		return
	}
	ip := client
	if r.URL.Path != "/" {
		ip, err = parseIP(strings.TrimPrefix(r.URL.Path, "/"))
		if err != nil {
			fail(w, 400, "Invalid IP address")
			return
		}
	}
	if !PublicIP(ip) {
		s.lookups.WithLabelValues("reserved").Inc()
		fail(w, 400, "Reserved or non-public IP address")
		return
	}
	rsp, err := s.Store.Lookup(ip)
	if rsp.Timings.Total > 0 {
		w.Header().Set("Server-Timing", rsp.Timings.ServerTiming())
		s.sourceDuration.WithLabelValues("total").Observe(rsp.Timings.Total.Seconds())
		for _, source := range []struct {
			name    string
			elapsed time.Duration
			called  bool
		}{{"maxmind", rsp.Timings.MaxMind, rsp.Timings.MaxMindCalled}, {"dbip", rsp.Timings.DBIP, rsp.Timings.DBIPCalled}, {"ipinfo", rsp.Timings.IPInfo, rsp.Timings.IPInfoCalled}} {
			if source.called {
				s.sourceDuration.WithLabelValues(source.name).Observe(source.elapsed.Seconds())
			}
		}
	}
	switch {
	case errors.Is(err, geo.ErrUnavailable):
		s.lookups.WithLabelValues("unavailable").Inc()
		fail(w, 503, "Databases unavailable")
	case errors.Is(err, geo.ErrNotFound):
		s.lookups.WithLabelValues("not_found").Inc()
		fail(w, 404, "IP information not found")
	case err != nil:
		s.lookups.WithLabelValues("error").Inc()
		slog.Error("database lookup failed")
		fail(w, 500, "Lookup failed")
	default:
		s.lookups.WithLabelValues("success").Inc()
		w.Header().Set("Link", s.attribution())
		if rsp.IPInfoUsed {
			w.Header().Set("Link", w.Header().Get("Link")+", <https://ipinfo.io/>; rel=\"attribution\"; title=\"IP address data powered by IPinfo\"")
		}
		rsp.Readme = origin + "/docs"
		if rsp.Flag.Image != nil {
			image := origin + *rsp.Flag.Image
			rsp.Flag.Image = &image
		}
		writeJSON(w, 200, rsp)
	}
}
func (s *Server) attribution() string {
	p := "<https://www.maxmind.com/>; rel=\"attribution\"; title=\"GeoLite2 data created by MaxMind\""
	dbip := "<https://db-ip.com/>; rel=\"attribution\"; title=\"IP Geolocation by DB-IP\""
	if s.Config.Provider == "dbip" {
		p = dbip
	} else {
		p += ", " + dbip
	}
	return p + ", <https://github.com/mledoze/countries>; rel=\"attribution\"; title=\"Country metadata, ODbL 1.0\""
}
func (s *Server) docs(w http.ResponseWriter) {
	writeJSON(w, 200, map[string]any{
		"build":        buildinfo.Current(),
		"name":         "myip",
		"routes":       []string{"GET /", "GET /<ip>", "GET /healthz", "GET /metrics (private metrics listener)", "GET /flags/<country>.svg", "GET /country-data.json"},
		"provider":     s.Config.Provider,
		"hybrid":       s.Config.Provider == "maxmind",
		"isp_from_asn": s.Config.ISPFromASN,
		"sources": map[string]string{
			"maxmind":                "https://www.maxmind.com/",
			"dbip":                   "https://db-ip.com/ (CC BY 4.0)",
			"ipinfo":                 "https://ipinfo.io/lite (CC BY-SA 4.0; optional network enrichment)",
			"countries":              "https://github.com/mledoze/countries (ODbL 1.0; unmodified snapshot at /country-data.json)",
			"flags":                  "https://github.com/lipis/flag-icons (MIT)",
			"timezone_compatibility": "https://github.com/eggert/tz (public-domain zone.tab)",
			"subdivision_codes":      "https://github.com/wooorm/iso-3166 (MIT)",
		},
		"notes": []string{
			"Locations are approximate, not GPS.",
			"Missing data is null. ASN owner is not necessarily the retail ISP.",
			"ISP_FROM_ASN explicitly uses the IPinfo network operator name as an ISP approximation, not independently verified retail ISP data.",
			"Server-Timing durations are milliseconds; lookup excludes HTTP serialization and network latency. Skipped sources are omitted.",
			"Missing region codes are supplemented only by an unambiguous country/name match in bundled ISO 3166-2 first-level subdivisions.",
			"maxmind selects a whole DB-IP fallback location when its own city is missing; dbip uses DB-IP only.",
			"A primary timezone is retained across fallback only for matching single-timezone countries with an exact IANA zone match.",
			"Timezone offsets are computed for the current date.",
			"Applications displaying DB-IP or hybrid results must link to https://db-ip.com/.",
		},
	})
}
func (s *Server) MetricsHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(s.Registry, promhttp.HandlerOpts{Timeout: 10 * time.Second, MaxRequestsInFlight: 4}))
	mux.HandleFunc("GET /databases", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		var update geo.UpdateStatus
		b, err := os.ReadFile(filepath.Join(s.Config.DataDir, "update-status.json"))
		if err == nil {
			err = json.Unmarshal(b, &update)
		}
		result := map[string]any{"ready": s.Store.Ready(), "updater_status_available": err == nil, "updater": update}
		if db, ok := s.Store.(*geo.Store); ok {
			m, _, _ := db.Stats()
			verified, at := db.IntegrityStats()
			result["loaded"] = m
			result["integrity_verified"] = verified
			result["last_integrity_verification"] = at
		}
		writeJSON(w, 200, result)
	})
	return mux
}
