package httpapi

import (
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestRateLimitEnableSwitch(t *testing.T) {
	t.Setenv("RATE_LIMIT_RPM", "1")
	t.Setenv("RATE_LIMIT_RPS", "1")
	t.Setenv("RATE_LIMIT_BURST", "1")
	t.Setenv("RATE_LIMIT_MAX_CLIENTS", "1")
	for _, value := range []string{"", "true", "false"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("RATELIMIT_ENABLE", value)
			c, err := LoadConfig()
			if err != nil {
				t.Fatal(err)
			}
			if c.RateLimitDisabled != (value == "false") {
				t.Fatal("incorrect enable switch")
			}
			s := New(c, nil)
			now := time.Now()
			ip := netip.MustParseAddr("8.8.8.8")
			if !s.limiter.Allow(ip, now) {
				t.Fatal("first request denied")
			}
			if value == "false" {
				for range 1000 {
					if ok, retry := s.limiter.AllowWithRetry(ip, now); !ok || retry != 0 {
						t.Fatal("disabled limiter denied a request")
					}
				}
				if !s.limiter.Allow(netip.MustParseAddr("1.1.1.1"), now) {
					t.Fatal("disabled capacity limit enforced")
				}
				if s.limiter.Stats() != (LimitStats{}) {
					t.Fatal("disabled limiter tracked clients")
				}
			} else if s.limiter.Allow(ip, now) {
				t.Fatal("enabled limiter bypassed")
			}
		})
	}
	t.Setenv("RATELIMIT_ENABLE", "invalid")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("invalid boolean accepted")
	}
}

func TestRPMDefaultsAndOptionalRPS(t *testing.T) {
	t.Setenv("RATE_LIMIT_RPM", "")
	t.Setenv("RATE_LIMIT_RPS", "")
	c, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.RPM != 300 || c.Rate != 0 {
		t.Fatal("unexpected default rate limits")
	}
	t.Setenv("RATE_LIMIT_RPM", "60")
	t.Setenv("RATE_LIMIT_RPS", "2")
	c, err = LoadConfig()
	if err != nil || c.RPM != 60 || c.Rate != 2 {
		t.Fatalf("combined limits not loaded: %v", err)
	}
	t.Setenv("RATE_LIMIT_RPM", "0")
	c, err = LoadConfig()
	if err != nil || c.RPM != 0 {
		t.Fatalf("zero request budget not accepted: %v", err)
	}
	for _, value := range []string{"-1", "NaN", "100001"} {
		t.Setenv("RATE_LIMIT_RPM", value)
		if _, err := LoadConfig(); err == nil {
			t.Fatalf("invalid RPM %s accepted", value)
		}
	}
}

func TestMetricsDefaultsToLoopback(t *testing.T) {
	t.Setenv("METRICS_ADDR", "")
	c, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.MetricsListen != "127.0.0.1:9090" {
		t.Fatal("local metrics exposed by default")
	}
}

func TestNoHardcodedOriginSettings(t *testing.T) {
	t.Setenv("BASE_URL", "not-a-url")
	t.Setenv("DOCS_URL", "not-a-url")
	if _, err := LoadConfig(); err != nil {
		t.Fatal("removed URL environment variables still affect configuration", err)
	}
	t.Setenv("ALLOWED_HOSTS", "ip.mynk.org,localhost:8080")
	c, err := LoadConfig()
	if err != nil || len(c.AllowedHosts) != 2 {
		t.Fatalf("allowlist not loaded: %v", err)
	}
	t.Setenv("ALLOWED_HOSTS", "https://ip.mynk.org/")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("URL accepted as host authority")
	}
}

func TestMappedTrustedProxyPrefixNormalization(t *testing.T) {
	for _, test := range []struct {
		prefix, normalized, peer string
	}{
		{"::ffff:192.0.2.5/120", "192.0.2.0/24", "[::ffff:192.0.2.5]:1234"},
		{"::ffff:192.0.2.5/128", "192.0.2.5/32", "192.0.2.5:1234"},
		{"::ffff:0.0.0.0/96", "0.0.0.0/0", "192.0.2.5:1234"},
		// A broader IPv6 prefix must not be widened into an IPv4 trust rule.
		{"::ffff:192.0.2.5/95", "::fffe:0:0/95", ""},
		{"2001:db8::5/64", "2001:db8::/64", "[2001:db8::5]:1234"},
	} {
		t.Run(test.prefix, func(t *testing.T) {
			t.Setenv("TRUSTED_PROXIES", test.prefix)
			t.Setenv("ALLOWED_HOSTS", "example.com")
			t.Setenv("PROXY_HEADER", "X-Forwarded-For")
			c, err := LoadConfig()
			if err != nil {
				t.Fatal(err)
			}
			if len(c.TrustedProxies) != 1 || c.TrustedProxies[0].String() != test.normalized {
				t.Fatalf("unexpected normalized prefixes: %v", c.TrustedProxies)
			}
			if test.peer == "" {
				return
			}
			r := httptest.NewRequest("GET", "http://internal/", nil)
			r.RemoteAddr = test.peer
			r.Header.Set("X-Forwarded-For", "8.8.8.8")
			r.Header.Set("X-Forwarded-Host", "example.com")
			r.Header.Set("X-Forwarded-Proto", "https")
			ip, err := ClientIP(r, c.TrustedProxies, c.ProxyHeader)
			if err != nil || ip.String() != "8.8.8.8" {
				t.Fatalf("forwarded client not trusted: %v %v", ip, err)
			}
			origin, err := requestOrigin(r, c)
			if err != nil || origin != "https://example.com" {
				t.Fatalf("forwarded origin not trusted: %q %v", origin, err)
			}
		})
	}
}
