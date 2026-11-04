package httpapi

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientIP(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}
	for _, tc := range []struct {
		name, remote, header, value, want string
		bad                               bool
	}{
		{"direct spoof ignored", "8.8.8.8:123", "X-Real-IP", "1.1.1.1", "8.8.8.8", false},
		{"trusted real IP", "10.0.0.2:123", "X-Real-IP", "1.1.1.1", "1.1.1.1", false},
		{"trusted CF header", "10.0.0.2:123", "CF-Connecting-IP", "2606:4700:4700::1111", "2606:4700:4700::1111", false},
		{"untrusted CF header", "8.8.8.8:123", "CF-Connecting-IP", "1.1.1.1", "8.8.8.8", false},
		{"chain spoof ignored", "10.0.0.2:123", "X-Forwarded-For", "9.9.9.9, 1.1.1.1, 10.0.0.3", "1.1.1.1", false},
		{"invalid trusted header", "10.0.0.2:123", "X-Real-IP", "not-an-ip", "", true},
		{"mapped IPv4", "[::ffff:8.8.8.8]:123", "X-Real-IP", "1.1.1.1", "8.8.8.8", false},
		{"IPv6 zone rejected", "[fe80::1%eth0]:123", "X-Real-IP", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.remote
			if tc.value != "" {
				r.Header.Set(tc.header, tc.value)
			}
			ip, err := ClientIP(r, trusted, tc.header)
			if tc.bad {
				if err == nil {
					t.Fatal("expected rejection")
				}
				return
			}
			if err != nil || ip.String() != tc.want {
				t.Fatalf("got %v %v, want %s", ip, err, tc.want)
			}
		})
	}
}
func TestPublicIP(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "100.64.1.2", "192.0.2.1", "198.18.0.1", "::1", "::", "fc00::1", "fe80::1", "2001:db8::1", "3fff::1", "64:ff9b::808:808", "224.0.0.1"} {
		if PublicIP(netip.MustParseAddr(raw)) {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, raw := range []string{"8.8.8.8", "1.1.1.1", "2401:4900:1ca2:48ff:5516:e857:6908:1227"} {
		if !PublicIP(netip.MustParseAddr(raw)) {
			t.Errorf("rejected %s", raw)
		}
	}
}
