// Package config holds shared, non-HTTP runtime settings and secret loading.
package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func Env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type Runtime struct {
	DataDir, Provider         string
	IPInfoEnabled, ISPFromASN bool
}

func LoadRuntime() (Runtime, error) {
	c := Runtime{DataDir: Env("DATA_DIR", "./data"), Provider: Env("GEOIP_PROVIDER", "maxmind")}
	if c.Provider != "maxmind" && c.Provider != "dbip" {
		return c, errors.New("GEOIP_PROVIDER must be maxmind or dbip")
	}
	var err error
	if c.Provider == "maxmind" {
		c.IPInfoEnabled, err = strconv.ParseBool(Env("IPINFO_ENABLED", "false"))
		if err != nil {
			return c, errors.New("IPINFO_ENABLED must be true or false")
		}
		c.ISPFromASN, err = strconv.ParseBool(Env("ISP_FROM_ASN", "false"))
		if err != nil {
			return c, errors.New("ISP_FROM_ASN must be true or false")
		}
	}
	c.DataDir, err = filepath.Abs(c.DataDir)
	if err != nil {
		return c, errors.New("invalid DATA_DIR")
	}
	return c, nil
}

type Updater struct {
	Runtime
	AccountID, LicenseKey, ConfigFile string
	IPInfoToken                       string
	Interval                          time.Duration
}

// Secret supports environment values or mounted files, never both. Errors name
// settings only and never include secret contents or file contents.
func Secret(key string) (string, error) {
	v, file := os.Getenv(key), os.Getenv(key+"_FILE")
	if v != "" && file != "" {
		return "", errors.New("set only one of " + key + " and " + key + "_FILE")
	}
	if file != "" {
		f, err := os.Open(file)
		if err != nil {
			return "", errors.New("cannot read " + key + "_FILE")
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, 4097))
		if err != nil || len(b) > 4096 {
			return "", errors.New("invalid " + key + "_FILE")
		}
		v = string(b)
	}
	v = strings.TrimSpace(v)
	if strings.ContainsAny(v, "\r\n\x00") || len(v) > 4096 {
		return "", errors.New("invalid " + key)
	}
	return v, nil
}

func LoadUpdater() (Updater, error) {
	r, err := LoadRuntime()
	c := Updater{Runtime: r}
	if err != nil {
		return c, err
	}
	c.Interval, err = time.ParseDuration(Env("UPDATE_INTERVAL", "24h"))
	if err != nil || c.Interval < time.Hour {
		return c, errors.New("UPDATE_INTERVAL must be at least 1h")
	}
	if c.Provider == "dbip" {
		return c, nil
	}
	if c.IPInfoEnabled {
		c.IPInfoToken, err = Secret("IPINFO_TOKEN")
		if err != nil {
			return c, err
		}
		if c.IPInfoToken == "" || strings.ContainsAny(c.IPInfoToken, " \t") {
			return c, errors.New("IPINFO_TOKEN is required when IPINFO_ENABLED=true")
		}
	}
	c.ConfigFile = os.Getenv("GEOIPUPDATE_CONFIG_FILE")
	c.AccountID, err = Secret("MAXMIND_ACCOUNT_ID")
	if err != nil {
		return c, err
	}
	c.LicenseKey, err = Secret("MAXMIND_LICENSE_KEY")
	if err != nil {
		return c, err
	}
	if c.ConfigFile != "" {
		if c.AccountID != "" || c.LicenseKey != "" {
			return c, errors.New("GEOIPUPDATE_CONFIG_FILE cannot be combined with MAXMIND credentials")
		}
		return c, nil
	}
	if id, err := strconv.ParseUint(c.AccountID, 10, 64); err != nil || id == 0 {
		return c, errors.New("MAXMIND_ACCOUNT_ID is required and must be a positive number")
	}
	if c.LicenseKey == "" || strings.ContainsAny(c.LicenseKey, " \t") {
		return c, errors.New("MAXMIND_LICENSE_KEY is required and must not contain whitespace")
	}
	return c, nil
}
