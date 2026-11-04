package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/TrueFix/myip/internal/geo"
)

func TestRequestOrigin(t *testing.T) {
	for _, tc := range []struct {
		name, url, peer, host, forwardHost, proto, want string
		trusted                                         bool
	}{
		{"local", "http://127.0.0.1:8080/", "8.8.8.8:123", "", "", "", "http://127.0.0.1:8080", false},
		{"TLS", "https://other.example:8443/", "8.8.8.8:123", "", "", "", "https://other.example:8443", false},
		{"IPv6", "http://[::1]:8080/", "8.8.8.8:123", "", "", "", "http://[::1]:8080", false},
		{"untrusted spoof ignored", "http://local.example/", "8.8.8.8:123", "", "evil.example", "https", "http://local.example", false},
		{"trusted origin", "http://api:8080/", "10.0.0.2:123", "", "ip.mynk.org", "https", "https://ip.mynk.org", true},
		{"trusted proto only", "http://public.example/", "10.0.0.2:123", "", "", "https", "https://public.example", true},
		{"bad scheme", "http://api/", "10.0.0.2:123", "", "", "javascript", "", true},
		{"ambiguous host", "http://api/", "10.0.0.2:123", "", "one.example, two.example", "https", "", true},
		{"ambiguous proto", "http://api/", "10.0.0.2:123", "", "", "https, http", "", true},
		{"bad authority", "http://api/", "8.8.8.8:123", "user@evil.example", "", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testConfig(t)
			if tc.trusted {
				c.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.2/32")}
			}
			r := httptest.NewRequest("GET", tc.url, nil)
			r.RemoteAddr = tc.peer
			if tc.host != "" {
				r.Host = tc.host
			}
			if tc.forwardHost != "" {
				r.Header.Set("X-Forwarded-Host", tc.forwardHost)
			}
			if tc.proto != "" {
				r.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			got, err := requestOrigin(r, c)
			if tc.want == "" {
				if err == nil {
					t.Fatal("invalid origin accepted")
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestOriginAllowlistAndDuplicates(t *testing.T) {
	c := testConfig(t)
	c.AllowedHosts = []string{"ip.mynk.org"}
	r := httptest.NewRequest("GET", "http://evil.example/", nil)
	r.RemoteAddr = "8.8.8.8:123"
	if _, err := requestOrigin(r, c); err == nil {
		t.Fatal("host allowlist bypassed")
	}
	r.Host = "IP.MYNK.ORG"
	if _, err := requestOrigin(r, c); err != nil {
		t.Fatal(err)
	}
	c.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("8.8.8.8/32")}
	r.Header.Add("X-Forwarded-Proto", "https")
	r.Header.Add("X-Forwarded-Proto", "http")
	if _, err := requestOrigin(r, c); err == nil {
		t.Fatal("duplicate forwarded header accepted")
	}
}

func TestLinksFollowEachRequest(t *testing.T) {
	s := New(testConfig(t), fakeStore{ready: true})
	h := s.Handler()
	for _, origin := range []string{"http://127.0.0.1:8080", "https://ip.mynk.org", "https://another.example:8443"} {
		r := httptest.NewRequest("GET", origin+"/1.1.1.1", nil)
		r.RemoteAddr = "8.8.8.8:123"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var response geo.Response
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Readme != origin+"/docs" || response.Flag.Image == nil || *response.Flag.Image != origin+"/flags/in.svg" {
			t.Fatalf("request-specific links incorrect: %+v", response)
		}
	}
}
