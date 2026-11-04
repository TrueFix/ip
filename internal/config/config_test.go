package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func clean(t *testing.T) {
	t.Helper()
	for _, key := range []string{"DATA_DIR", "GEOIP_PROVIDER", "UPDATE_INTERVAL", "MAXMIND_ACCOUNT_ID", "MAXMIND_LICENSE_KEY", "MAXMIND_ACCOUNT_ID_FILE", "MAXMIND_LICENSE_KEY_FILE", "GEOIPUPDATE_CONFIG_FILE", "IPINFO_ENABLED", "IPINFO_TOKEN", "IPINFO_TOKEN_FILE", "ISP_FROM_ASN"} {
		t.Setenv(key, "")
	}
}

func TestIPInfoTokenValidation(t *testing.T) {
	clean(t)
	t.Setenv("MAXMIND_ACCOUNT_ID", "123")
	t.Setenv("MAXMIND_LICENSE_KEY", "test-token")
	t.Setenv("IPINFO_ENABLED", "true")
	if _, err := LoadUpdater(); err == nil || !strings.Contains(err.Error(), "IPINFO_TOKEN") {
		t.Fatal("missing IPinfo token accepted")
	}
	t.Setenv("IPINFO_TOKEN", "another-test-token")
	c, err := LoadUpdater()
	if err != nil || c.IPInfoToken != "another-test-token" {
		t.Fatalf("IPinfo token not loaded: %v", err)
	}
	t.Setenv("GEOIP_PROVIDER", "dbip")
	t.Setenv("IPINFO_TOKEN_FILE", "/does/not/exist")
	c, err = LoadUpdater()
	if err != nil || c.IPInfoEnabled || c.IPInfoToken != "" {
		t.Fatal("DB-IP-only mode accessed IPinfo")
	}
}

func TestDefaultDataDirectory(t *testing.T) {
	clean(t)
	dir := t.TempDir()
	t.Chdir(dir)
	c, err := LoadRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if c.DataDir != filepath.Join(dir, "data") {
		t.Fatal(c.DataDir)
	}
}
func TestUpdaterCredentials(t *testing.T) {
	clean(t)
	t.Setenv("MAXMIND_ACCOUNT_ID", "123")
	t.Setenv("MAXMIND_LICENSE_KEY", "test-token")
	c, err := LoadUpdater()
	if err != nil {
		t.Fatal(err)
	}
	if c.AccountID != "123" || c.LicenseKey != "test-token" {
		t.Fatal("credentials not loaded")
	}
	t.Setenv("MAXMIND_ACCOUNT_ID", "000")
	if _, err := LoadUpdater(); err == nil {
		t.Fatal("zero account accepted")
	}
	t.Setenv("MAXMIND_ACCOUNT_ID", "123")
	t.Setenv("MAXMIND_LICENSE_KEY", "test\nEditionIDs bad")
	if _, err := LoadUpdater(); err == nil {
		t.Fatal("configuration injection accepted")
	}
}

func TestMaxMindMissingCredentialsFailFast(t *testing.T) {
	for _, tc := range []struct{ name, id, key, setting string }{
		{"neither", "", "", "MAXMIND_ACCOUNT_ID"},
		{"missing id", "", "test-token", "MAXMIND_ACCOUNT_ID"},
		{"missing key", "123", "", "MAXMIND_LICENSE_KEY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clean(t)
			t.Setenv("GEOIP_PROVIDER", "maxmind")
			t.Setenv("MAXMIND_ACCOUNT_ID", tc.id)
			t.Setenv("MAXMIND_LICENSE_KEY", tc.key)
			_, err := LoadUpdater()
			if err == nil || !strings.Contains(err.Error(), tc.setting) {
				t.Fatalf("expected clear missing-credential error, got %v", err)
			}
		})
	}
	clean(t)
	t.Setenv("GEOIP_PROVIDER", "madmin")
	if _, err := LoadUpdater(); err == nil || !strings.Contains(err.Error(), "GEOIP_PROVIDER") {
		t.Fatal("provider typo not rejected")
	}
}
func TestSecretFilesAndConflicts(t *testing.T) {
	clean(t)
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("test-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAXMIND_ACCOUNT_ID", "123")
	t.Setenv("MAXMIND_LICENSE_KEY_FILE", path)
	c, err := LoadUpdater()
	if err != nil || c.LicenseKey != "test-token" {
		t.Fatalf("file credential not loaded: %v", err)
	}
	t.Setenv("MAXMIND_LICENSE_KEY", "private-value")
	_, err = LoadUpdater()
	if err == nil || strings.Contains(err.Error(), "private-value") {
		t.Fatal("conflict accepted or secret leaked")
	}
}
func TestDBIPDoesNotRequireOrReadMaxMindSecrets(t *testing.T) {
	clean(t)
	t.Setenv("GEOIP_PROVIDER", "dbip")
	t.Setenv("MAXMIND_LICENSE_KEY_FILE", "/does/not/exist")
	if _, err := LoadUpdater(); err != nil {
		t.Fatal(err)
	}
}
func TestLegacyConfigurationIsExplicit(t *testing.T) {
	clean(t)
	if _, err := LoadUpdater(); err == nil {
		t.Fatal("missing credentials accepted")
	}
	t.Setenv("GEOIPUPDATE_CONFIG_FILE", "/legacy/GeoIP.conf")
	if _, err := LoadUpdater(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MAXMIND_ACCOUNT_ID", "123")
	if _, err := LoadUpdater(); err == nil {
		t.Fatal("mixed legacy/new credentials accepted")
	}
}
