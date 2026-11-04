// Package inventory describes and verifies immutable, local database snapshots.
package inventory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oschwald/maxminddb-golang"
)

type Spec struct{ Provider, Edition, Path string }
type File struct {
	Provider     string    `json:"provider"`
	Edition      string    `json:"edition"`
	Path         string    `json:"path"`
	SHA256       string    `json:"sha256"`
	Size         int64     `json:"size_bytes"`
	BuildEpoch   uint      `json:"build_epoch"`
	DownloadedAt time.Time `json:"downloaded_at"`
	CheckedAt    time.Time `json:"checked_at"`
	VerifiedAt   time.Time `json:"verified_at"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
	SourceRef    string    `json:"source_ref"`
	Action       string    `json:"action"`
}

func Specs(provider string, hybrid, ipinfo bool) []Spec {
	out := []Spec{{provider, "city", "City.mmdb"}, {provider, "asn", "ASN.mmdb"}}
	if provider == "maxmind" && hybrid {
		out = append(out, Spec{"dbip", "city", "dbip/City.mmdb"}, Spec{"dbip", "asn", "dbip/ASN.mmdb"})
	}
	if provider == "maxmind" && ipinfo {
		out = append(out, Spec{"ipinfo", "lite", "IPinfo.mmdb"})
	}
	return out
}

func Digest(path string) (string, int64, error) {
	link, err := os.Lstat(path)
	if err != nil || !link.Mode().IsRegular() {
		return "", 0, errors.New("database is not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", 0, errors.New("database is not a regular file")
	}
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func Inspect(root string, s Spec) (File, error) {
	path := filepath.Join(root, filepath.FromSlash(s.Path))
	hash, size, err := Digest(path)
	if err != nil {
		return File{}, err
	}
	db, err := maxminddb.Open(path)
	if err != nil {
		return File{}, err
	}
	epoch := db.Metadata.BuildEpoch
	db.Close()
	return File{Provider: s.Provider, Edition: s.Edition, Path: s.Path, SHA256: hash, Size: size, BuildEpoch: epoch, VerifiedAt: time.Now().UTC()}, nil
}

func Matches(path string, f File) bool {
	hash, size, err := Digest(path)
	return err == nil && hash == f.SHA256 && size == f.Size
}

// Verify requires exactly the expected files; metadata may not introduce paths.
// SHA-256 detects changes relative to the locally trusted manifest, not a vendor
// signature. The data directory and manifest must be protected from writes.
func Verify(root string, files []File, specs []Spec) error {
	if len(files) != len(specs) {
		return errors.New("snapshot inventory is incomplete")
	}
	expected := map[string]Spec{}
	for _, s := range specs {
		expected[s.Path] = s
	}
	for _, f := range files {
		s, ok := expected[f.Path]
		if !ok || s.Provider != f.Provider || s.Edition != f.Edition {
			return errors.New("unexpected database inventory entry")
		}
		decoded, err := hex.DecodeString(f.SHA256)
		if err != nil || len(decoded) != sha256.Size || strings.ToLower(f.SHA256) != f.SHA256 {
			return errors.New("invalid database checksum")
		}
		if !Matches(filepath.Join(root, filepath.FromSlash(f.Path)), f) {
			return errors.New("database checksum or size mismatch")
		}
		delete(expected, f.Path)
	}
	return nil
}
