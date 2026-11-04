package httpapi

import (
	"fmt"
	"github.com/TrueFix/myip/internal/config"
	"math"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	ISPFromASN                                            bool
	Listen, MetricsListen, DataDir, Provider, ProxyHeader string
	TrustedProxies                                        []netip.Prefix
	Rate                                                  float64
	Burst, MaxClients                                     int
	RPM                                                   int
	AllowedHosts                                          []string
	CORSOrigin                                            string
}

func Env(key, fallback string) string {
	return config.Env(key, fallback)
}
func LoadConfig() (Config, error) {
	r, err := config.LoadRuntime()
	if err != nil {
		return Config{}, err
	}
	c := Config{Listen: Env("LISTEN_ADDR", ":8080"), MetricsListen: Env("METRICS_ADDR", "127.0.0.1:9090"), DataDir: r.DataDir, Provider: r.Provider, ISPFromASN: r.ISPFromASN, ProxyHeader: Env("PROXY_HEADER", "X-Forwarded-For"), CORSOrigin: Env("CORS_ORIGIN", "*")}
	for _, raw := range strings.Split(os.Getenv("TRUSTED_PROXIES"), ",") {
		if raw = strings.TrimSpace(raw); raw != "" {
			p, err := netip.ParsePrefix(raw)
			if err != nil {
				return c, fmt.Errorf("invalid trusted proxy CIDR")
			}
			c.TrustedProxies = append(c.TrustedProxies, p.Masked())
		}
	}
	switch strings.ToLower(c.ProxyHeader) {
	case "x-forwarded-for", "x-real-ip", "cf-connecting-ip":
	default:
		return c, fmt.Errorf("unsupported PROXY_HEADER")
	}
	c.RPM, err = strconv.Atoi(Env("RATE_LIMIT_RPM", "300"))
	if err != nil || c.RPM < 0 || c.RPM > 100000 {
		return c, fmt.Errorf("RATE_LIMIT_RPM must be in [0,100000] (0 disables all rate limiting)")
	}
	if raw := os.Getenv("RATE_LIMIT_RPS"); raw != "" {
		c.Rate, err = strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(c.Rate) || math.IsInf(c.Rate, 0) || c.Rate <= 0 || c.Rate > 100000 {
			return c, fmt.Errorf("RATE_LIMIT_RPS must be in (0,100000] when set")
		}
	}
	c.Burst, err = strconv.Atoi(Env("RATE_LIMIT_BURST", "20"))
	if err != nil || c.Burst < 1 || c.Burst > 100000 {
		return c, fmt.Errorf("invalid RATE_LIMIT_BURST")
	}
	c.MaxClients, err = strconv.Atoi(Env("RATE_LIMIT_MAX_CLIENTS", "50000"))
	if err != nil || c.MaxClients < 1 || c.MaxClients > 1000000 {
		return c, fmt.Errorf("invalid RATE_LIMIT_MAX_CLIENTS")
	}
	for _, host := range strings.Split(os.Getenv("ALLOWED_HOSTS"), ",") {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		if !validAuthority(host) {
			return c, fmt.Errorf("invalid ALLOWED_HOSTS authority")
		}
		c.AllowedHosts = append(c.AllowedHosts, strings.ToLower(host))
	}
	if strings.ContainsAny(c.CORSOrigin, "\r\n") {
		return c, fmt.Errorf("invalid CORS_ORIGIN")
	}
	return c, nil
}
