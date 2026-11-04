package geo

import (
	"strings"
	"testing"
)

func TestValidVersion(t *testing.T) {
	for _, v := range []string{"maxmind-2026-11-123", "dbip-2026-11-456", "test-version"} {
		if !ValidVersion(v) {
			t.Fatal(v)
		}
	}
	for _, v := range []string{"", ".", "..", "../outside", "dbip-2026-11-../../outside", "/absolute", `maxmind-\outside`, strings.Repeat("a", 129)} {
		if ValidVersion(v) {
			t.Fatalf("unsafe version accepted: %q", v)
		}
	}
}
