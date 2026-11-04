package inventory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegrityAndPathValidation(t *testing.T) {
	dir := t.TempDir()
	specs := Specs("dbip", false, false)
	files := []File{}
	for _, s := range specs {
		source := "GeoIP2-City-Test.mmdb"
		if s.Edition == "asn" {
			source = "GeoLite2-ASN-Test.mmdb"
		}
		b, err := os.ReadFile(filepath.Join("..", "geo", "testdata", source))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, s.Path), b, 0644); err != nil {
			t.Fatal(err)
		}
		f, err := Inspect(dir, s)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	if err := Verify(dir, files, specs); err != nil {
		t.Fatal(err)
	}
	files[0].SHA256 = strings.Repeat("0", 64)
	if Verify(dir, files, specs) == nil {
		t.Fatal("checksum mismatch accepted")
	}
	files[0].Path = "../outside"
	if Verify(dir, files, specs) == nil {
		t.Fatal("outside path accepted")
	}
	if Verify(dir, files[:1], specs) == nil {
		t.Fatal("missing database accepted")
	}
}
