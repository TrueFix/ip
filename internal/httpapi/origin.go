package httpapi

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

func validAuthority(authority string) bool {
	if authority == "" || len(authority) > 320 || strings.ContainsAny(authority, "/\\@?#, \t\r\n") {
		return false
	}
	host := authority
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		ip, err := netip.ParseAddr(host[1 : len(host)-1])
		return err == nil && ip.Is6() && ip.Zone() == ""
	}
	if strings.Contains(host, ":") {
		var port string
		var err error
		host, port, err = net.SplitHostPort(authority)
		if err != nil {
			return false
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.Zone() == ""
	}
	host = strings.TrimSuffix(host, ".")
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return false
			}
		}
	}
	return true
}

func trustedPeer(r *http.Request, trusted []netip.Prefix) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip, err := parseIP(host)
	return err == nil && inPrefixes(ip, trusted)
}

func singleHeader(r *http.Request, name string) (string, error) {
	values := r.Header.Values(name)
	if len(values) == 0 {
		return "", nil
	}
	if len(values) != 1 || strings.Contains(values[0], ",") || strings.TrimSpace(values[0]) == "" {
		return "", errors.New("ambiguous forwarded origin header")
	}
	return strings.TrimSpace(values[0]), nil
}

func requestOrigin(r *http.Request, c Config) (string, error) {
	host, scheme := r.Host, "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if trustedPeer(r, c.TrustedProxies) {
		forwardedHost, err := singleHeader(r, "X-Forwarded-Host")
		if err != nil {
			return "", err
		}
		proto, err := singleHeader(r, "X-Forwarded-Proto")
		if err != nil {
			return "", err
		}
		if forwardedHost != "" {
			host = forwardedHost
		}
		if proto != "" {
			if proto != "http" && proto != "https" {
				return "", errors.New("invalid forwarded scheme")
			}
			scheme = proto
		}
	}
	if !validAuthority(host) {
		return "", errors.New("invalid request host")
	}
	if len(c.AllowedHosts) > 0 {
		allowed := false
		for _, candidate := range c.AllowedHosts {
			if strings.EqualFold(host, candidate) {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", errors.New("request host not allowed")
		}
	}
	return scheme + "://" + host, nil
}
