package updater

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/TrueFix/myip/internal/geo"
	"github.com/TrueFix/myip/internal/inventory"
)

type tracking struct {
	root, stage string
	current     geo.Manifest
	previous    map[string]inventory.File
	transfers   map[string]inventory.File
}

func newTracking(root, stage string, m geo.Manifest) *tracking {
	t := &tracking{root: root, stage: stage, current: m, previous: map[string]inventory.File{}, transfers: map[string]inventory.File{}}
	// Do not trust arbitrary metadata paths from a manually modified manifest.
	for _, s := range inventory.Specs(m.Provider, m.Hybrid, m.IPInfo) {
		for _, f := range m.Files {
			if f.Path == s.Path && f.Provider == s.Provider && f.Edition == s.Edition {
				t.previous[f.Path] = f
			}
		}
	}
	return t
}
func (t *tracking) relative(path string) string {
	rel, _ := filepath.Rel(t.stage, path)
	return filepath.ToSlash(rel)
}

func (t *tracking) collect(provider string, hybrid, ipinfo bool) ([]inventory.File, error) {
	files := []inventory.File{}
	now := time.Now().UTC()
	for _, spec := range inventory.Specs(provider, hybrid, ipinfo) {
		f, err := inventory.Inspect(t.stage, spec)
		if err != nil {
			return nil, err
		}
		old, exists := t.previous[f.Path]
		if exists && old.BuildEpoch > f.BuildEpoch {
			return nil, errors.New("database build timestamp moved backwards")
		}
		if transfer, ok := t.transfers[f.Path]; ok {
			f.DownloadedAt = transfer.DownloadedAt
			f.CheckedAt = transfer.CheckedAt
			f.ETag = transfer.ETag
			f.LastModified = transfer.LastModified
			f.SourceRef = transfer.SourceRef
			f.Action = transfer.Action
		} else {
			// geoipupdate handles its own conditional protocol and does not expose
			// HTTP validators. Preserve known dates for unchanged bytes.
			f.CheckedAt = now
			f.SourceRef = "geoipupdate/GeoLite2-" + strings.TrimSuffix(filepath.Base(f.Path), ".mmdb")
			f.Action = "changed"
			f.DownloadedAt = now
			if exists && old.SHA256 == f.SHA256 {
				f.DownloadedAt = old.DownloadedAt
				f.Action = "unchanged"
			} else if !exists && geo.ValidVersion(t.current.Version) {
				hash, _, err := inventory.Digest(filepath.Join(t.root, t.current.Version, filepath.FromSlash(f.Path)))
				if err == nil && hash == f.SHA256 {
					f.DownloadedAt = time.Time{}
					f.Action = "imported"
				}
			}
		}
		files = append(files, f)
	}
	return files, nil
}

func sourceRef(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.User = nil
	return u.String()
}
func validator(raw, secret string) string {
	if len(raw) > 512 || strings.ContainsAny(raw, "\r\n\x00") || (secret != "" && strings.Contains(raw, secret)) {
		return ""
	}
	return raw
}

func (u *Updater) fetch(ctx context.Context, endpoint, path string, compressed bool, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return errors.New("invalid download endpoint")
	}
	var old inventory.File
	cached := ""
	rel := ""
	ref := sourceRef(endpoint)
	if u.tracking != nil {
		rel = u.tracking.relative(path)
		candidate, ok := u.tracking.previous[rel]
		if ok && candidate.SourceRef == ref && geo.ValidVersion(u.tracking.current.Version) {
			previousPath := filepath.Join(u.Dir, u.tracking.current.Version, filepath.FromSlash(rel))
			if inventory.Matches(previousPath, candidate) {
				old = candidate
				cached = previousPath
				if old.ETag != "" {
					req.Header.Set("If-None-Match", old.ETag)
				}
				if old.LastModified != "" {
					req.Header.Set("If-Modified-Since", old.LastModified)
				}
			}
		}
	}
	client := u.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("database download request failed")
	}
	defer resp.Body.Close()
	now := time.Now().UTC()
	record := inventory.File{SourceRef: ref, CheckedAt: now, DownloadedAt: now, ETag: validator(resp.Header.Get("ETag"), u.IPInfoToken), LastModified: validator(resp.Header.Get("Last-Modified"), u.IPInfoToken), Action: "downloaded"}
	if resp.StatusCode == http.StatusNotModified {
		if cached == "" || (req.Header.Get("If-None-Match") == "" && req.Header.Get("If-Modified-Since") == "") {
			return errors.New("unexpected 304 without a verified cached file")
		}
		if err := copyFile(cached, path); err != nil {
			return err
		}
		record.DownloadedAt = old.DownloadedAt
		record.Action = "not_modified"
		if record.ETag == "" {
			record.ETag = old.ETag
		}
		if record.LastModified == "" {
			record.LastModified = old.LastModified
		}
	} else {
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("database download returned HTTP %d", resp.StatusCode)
		}
		var reader io.Reader = resp.Body
		if compressed {
			z, err := gzip.NewReader(io.LimitReader(resp.Body, 512<<20))
			if err != nil {
				return errors.New("invalid compressed database")
			}
			defer z.Close()
			reader = z
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		n, err := io.Copy(f, io.LimitReader(reader, limit+1))
		ce := f.Close()
		if err != nil || ce != nil {
			return errors.New("database download could not be written")
		}
		if n > limit {
			return errors.New("database exceeds size limit")
		}
	}
	if u.tracking != nil {
		u.tracking.transfers[rel] = record
	}
	return nil
}
