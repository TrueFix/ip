package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/TrueFix/myip/internal/geo"
)

type fakeStore struct {
	ready bool
	err   error
}

type timedStore struct{ fakeStore }

func (t timedStore) Lookup(ip netip.Addr) (geo.Response, error) {
	r, err := t.fakeStore.Lookup(ip)
	r.Timings = geo.LookupTimings{Total: 10 * time.Microsecond, MaxMind: 2 * time.Microsecond, DBIP: 3 * time.Microsecond, IPInfo: time.Microsecond, MaxMindCalled: true, DBIPCalled: true, IPInfoCalled: true}
	r.IPInfoUsed = true
	return r, err
}

func TestServerTimingAndIPInfoAttribution(t *testing.T) {
	c := testConfig(t)
	c.Provider = "maxmind"
	c.CORSOrigin = "*"
	s := New(c, timedStore{fakeStore{ready: true}})
	w := request(s.Handler(), "/1.1.1.1")
	for _, source := range []string{"lookup", "maxmind", "dbip", "ipinfo"} {
		if !strings.Contains(w.Header().Get("Server-Timing"), source+";dur=") {
			t.Fatal("missing stage timing")
		}
	}
	if !strings.Contains(w.Header().Get("Link"), "https://ipinfo.io/") || !strings.Contains(w.Header().Get("Access-Control-Expose-Headers"), "Server-Timing") {
		t.Fatal("missing IPinfo attribution or exposed timing header")
	}
	metrics := request(s.MetricsHandler(), "/metrics").Body.String()
	if !strings.Contains(metrics, `myip_geoip_lookup_duration_seconds_count{source="ipinfo"} 1`) {
		t.Fatal("missing source timing metric")
	}
}

func (f fakeStore) Ready() bool { return f.ready }
func (f fakeStore) Lookup(ip netip.Addr) (geo.Response, error) {
	image := "/flags/in.svg"
	return geo.Response{IP: ip.String(), Success: true, Flag: geo.Flag{Image: &image}}, f.err
}
func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{Provider: "dbip", ProxyHeader: "X-Forwarded-For", RPM: 300, Rate: 5, Burst: 100, MaxClients: 10, DataDir: t.TempDir()}
}
func request(h http.Handler, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	r.RemoteAddr = "8.8.8.8:12345"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestDisabledRateLimitsAllowRepeatedHTTPRequests(t *testing.T) {
	c := testConfig(t)
	c.RateLimitDisabled = true
	c.RPM, c.Rate, c.Burst, c.MaxClients = 1, 1, 1, 1
	s := New(c, fakeStore{ready: true})
	h := s.Handler()
	for i := range 100 {
		r := httptest.NewRequest("GET", "/8.8.8.8", nil)
		if i%2 == 0 {
			r.RemoteAddr = "8.8.8.8:12345"
		} else {
			r.RemoteAddr = "1.1.1.1:12345"
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK || w.Header().Get("Retry-After") != "" {
			t.Fatalf("disabled limits affected HTTP response: %d %s", w.Code, w.Body.String())
		}
	}
	if s.limiter.Stats() != (LimitStats{}) {
		t.Fatal("disabled HTTP limiter tracked clients")
	}
}

func TestZeroRPMHTTPBehavior(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		c := testConfig(t)
		c.RPM = 0
		c.RateLimitDisabled = disabled
		s := New(c, fakeStore{ready: true})
		h := s.Handler()
		for range 3 {
			w := request(h, "/8.8.8.8")
			if disabled {
				if w.Code != http.StatusOK {
					t.Fatalf("disabled zero-RPM lookup: %d", w.Code)
				}
			} else if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
				t.Fatalf("enabled zero-RPM lookup: %d, Retry-After=%s", w.Code, w.Header().Get("Retry-After"))
			}
		}
		if w := request(h, "/healthz"); w.Code != http.StatusOK {
			t.Fatalf("zero RPM blocked readiness: %d", w.Code)
		}
		if w := request(s.MetricsHandler(), "/metrics"); w.Code != http.StatusOK {
			t.Fatalf("zero RPM blocked metrics: %d", w.Code)
		}
	}
}
func TestRoutes(t *testing.T) {
	s := New(testConfig(t), fakeStore{ready: true})
	h := s.Handler()
	for _, tc := range []struct {
		path string
		code int
	}{{"/", 200}, {"/1.1.1.1", 200}, {"/2606:4700:4700::1111", 200}, {"/healthz", 200}, {"/metrics", 404}, {"/docs", 200}, {"/flags/in.svg", 200}, {"/flags/zz.svg", 404}, {"/country-data.json", 200}, {"/invalid", 400}, {"/127.0.0.1", 400}, {"/a/b", 404}} {
		w := request(h, tc.path)
		if w.Code != tc.code {
			t.Errorf("%s: got %d want %d (%s)", tc.path, w.Code, tc.code, w.Body.String())
		}
	}
	w := request(h, "/")
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("personalized response cacheable")
	}
	if !strings.Contains(w.Header().Get("Link"), "https://db-ip.com/") {
		t.Fatal("missing attribution")
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ip"] != "8.8.8.8" {
		t.Fatal(payload)
	}
	r := httptest.NewRequest("POST", "/1.1.1.1", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
	w = request(s.MetricsHandler(), "/metrics")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "myip_http_requests_total") {
		t.Fatal("metrics missing")
	}
	if strings.Contains(w.Body.String(), "1.1.1.1") {
		t.Fatal("IP leaked to metrics")
	}
	private := request(s.MetricsHandler(), "/databases")
	if private.Code != 200 || !strings.Contains(private.Body.String(), "updater_status_available") {
		t.Fatal("private inventory endpoint missing")
	}
	public := request(h, "/databases")
	if public.Code == 200 {
		t.Fatal("inventory exposed on public API")
	}
}
func TestFailureStatus(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{{geo.ErrUnavailable, 503}, {geo.ErrNotFound, 404}} {
		s := New(testConfig(t), fakeStore{err: tc.err})
		if got := request(s.Handler(), "/1.1.1.1").Code; got != tc.status {
			t.Fatal(got)
		}
	}
	s := New(testConfig(t), fakeStore{})
	if got := request(s.Handler(), "/healthz").Code; got != 503 {
		t.Fatal(got)
	}
}
func TestRateLimit(t *testing.T) {
	c := testConfig(t)
	c.Burst = 1
	s := New(c, fakeStore{ready: true})
	h := s.Handler()
	if request(h, "/").Code != 200 {
		t.Fatal("first request failed")
	}
	w := request(h, "/")
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatal("not rate limited")
	}
}

func TestHybridAttribution(t *testing.T) {
	c := testConfig(t)
	c.Provider = "maxmind"
	s := New(c, fakeStore{ready: true})
	w := request(s.Handler(), "/1.1.1.1")
	link := w.Header().Get("Link")
	if !strings.Contains(link, "https://www.maxmind.com/") || !strings.Contains(link, "https://db-ip.com/") {
		t.Fatal("hybrid response missing provider attribution")
	}
	c.Provider = "dbip"
	s = New(c, fakeStore{ready: true})
	w = request(s.Handler(), "/1.1.1.1")
	if strings.Contains(w.Header().Get("Link"), "maxmind.com") {
		t.Fatal("DB-IP-only response credits unused provider")
	}
}
func TestLimiterCapacityAndIPv6(t *testing.T) {
	l := NewLimiter(300, 1, 1, 1)
	now := time.Now()
	if !l.Allow(netip.MustParseAddr("2001:4860::1"), now) {
		t.Fatal("initial denial")
	}
	if l.Allow(netip.MustParseAddr("2001:4860::2"), now) {
		t.Fatal("IPv6 rotation bypass")
	}
	if l.Allow(netip.MustParseAddr("8.8.8.8"), now) {
		t.Fatal("capacity exceeded")
	}
	if !l.Allow(netip.MustParseAddr("8.8.8.8"), now.Add(11*time.Minute)) {
		t.Fatal("idle entries not cleaned")
	}
}
func TestConfigRejectsNaNAndTrustAllIsExplicit(t *testing.T) {
	t.Setenv("RATE_LIMIT_RPS", "NaN")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("accepted NaN")
	}
	t.Setenv("RATE_LIMIT_RPS", "5")
	t.Setenv("TRUSTED_PROXIES", "")
	c, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.TrustedProxies) != 0 {
		t.Fatal("trusted proxies enabled by default")
	}
}
