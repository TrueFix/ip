package httpapi

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

func inPrefixes(ip netip.Addr, prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
func parseIP(raw string) (netip.Addr, error) {
	ip, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil || ip.Zone() != "" {
		return netip.Addr{}, errors.New("invalid IP address")
	}
	return ip.Unmap(), nil
}
func ClientIP(r *http.Request, trusted []netip.Prefix, header string) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}, errors.New("invalid remote address")
	}
	peer, err := parseIP(host)
	if err != nil {
		return netip.Addr{}, err
	}
	if !inPrefixes(peer, trusted) {
		return peer, nil
	}
	values := r.Header.Values(header)
	if len(values) == 0 {
		return peer, nil
	}
	if !strings.EqualFold(header, "X-Forwarded-For") {
		if len(values) != 1 {
			return netip.Addr{}, errors.New("ambiguous proxy header")
		}
		return parseIP(values[0])
	}
	chain := strings.Split(strings.Join(values, ","), ",")
	if len(chain) > 32 {
		return netip.Addr{}, errors.New("proxy chain too long")
	}
	for i := len(chain) - 1; i >= 0; i-- {
		if !inPrefixes(peer, trusted) {
			return peer, nil
		}
		peer, err = parseIP(chain[i])
		if err != nil {
			return netip.Addr{}, err
		}
	}
	return peer, nil
}

// Special-purpose networks that must not be treated as public client locations.
var reserved = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "::/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20", "fc00::/7", "fe80::/10", "ff00::/8"} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

func PublicIP(ip netip.Addr) bool {
	return ip.IsValid() && ip.IsGlobalUnicast() && !inPrefixes(ip, reserved)
}
