package buildinfo

import (
	"strings"
	"testing"
)

func TestVersionMetadata(t *testing.T) {
	info := Current()
	if info.Version == "" || info.Revision == "" || info.GoVersion == "" || !strings.Contains(String(), info.Version) {
		t.Fatal("missing build information")
	}
}
