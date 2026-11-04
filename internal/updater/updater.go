// Package updater publishes validated database pairs through an atomic manifest.
// Run exactly one updater per data volume; API replicas may share the volume read-only.
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/TrueFix/myip/internal/audit"
	"github.com/TrueFix/myip/internal/geo"
	"github.com/gofrs/flock"
)

type Updater struct {
	Dir, Provider, ConfigFile string
	AccountID, LicenseKey     string
	IPInfoToken               string
	Client                    *http.Client
	tracking                  *tracking
}

var ErrUpdateInProgress = errors.New("another updater already holds the data-directory lock")

func AtomicJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".json-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0644); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(name, path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
func (u *Updater) Run(ctx context.Context) (result error) {
	if u.Provider != "maxmind" && u.Provider != "dbip" {
		return errors.New("unknown provider")
	}
	if err := os.MkdirAll(u.Dir, 0755); err != nil {
		return err
	}
	lock := flock.New(filepath.Join(u.Dir, ".updater.lock"))
	defer lock.Close()
	locked, err := lock.TryLock()
	if err != nil {
		return errors.New("cannot lock database directory")
	}
	if !locked {
		return ErrUpdateInProgress
	}
	var status geo.UpdateStatus
	b, _ := os.ReadFile(filepath.Join(u.Dir, "update-status.json"))
	_ = json.Unmarshal(b, &status)
	status.LastAttempt = time.Now().UTC()
	defer func() {
		if result != nil {
			status.Failures++
		} else {
			status.LastSuccess = time.Now().UTC()
		}
		if err := AtomicJSON(filepath.Join(u.Dir, "update-status.json"), status); err != nil && result == nil {
			result = err
		}
	}()
	var current geo.Manifest
	b, _ = os.ReadFile(filepath.Join(u.Dir, "current.json"))
	_ = json.Unmarshal(b, &current)
	if !geo.ValidVersion(current.Version) {
		current = geo.Manifest{}
	}
	month := time.Now().UTC().Format("2006-01")
	db, err := audit.Open(u.Dir)
	if err != nil {
		return errors.New("cannot open update audit database")
	}
	defer db.Close()
	if err := db.Recover(current.Version, current.AuditRunID); err != nil {
		return errors.New("cannot recover update audit")
	}
	runID, err := db.Begin(u.Provider)
	if err != nil {
		return errors.New("cannot begin update audit")
	}
	publishedID := current.AuditRunID
	defer func() {
		if err := db.Finish(runID, result == nil, publishedID); err != nil && result == nil {
			result = errors.New("cannot finalize update audit")
		}
	}()
	stage, err := os.MkdirTemp(u.Dir, ".staging-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	u.tracking = newTracking(u.Dir, stage, current)
	defer func() { u.tracking = nil }()
	if err = os.Chmod(stage, 0755); err != nil {
		return err
	}
	if u.Provider == "maxmind" {
		configFile, cleanup, err := u.maxmindConfig()
		if err != nil {
			return err
		}
		defer cleanup()
		// Seed the updater so its conditional checks can avoid unnecessary downloads.
		if current.Provider == "maxmind" && filepath.Base(current.Version) == current.Version && current.Version != "." && current.Version != ".." {
			for _, edition := range []string{"City", "ASN"} {
				src := filepath.Join(u.Dir, current.Version, edition+".mmdb")
				if _, err := os.Stat(src); err == nil {
					if err := copyFile(src, filepath.Join(stage, "GeoLite2-"+edition+".mmdb")); err != nil {
						return err
					}
				}
			}
		}
		cmd := exec.CommandContext(ctx, "geoipupdate", "-f", configFile, "-d", stage)
		// Do not forward child output: upstream errors may contain sensitive configuration.
		if err := cmd.Run(); err != nil {
			return errors.New("geoipupdate failed; check credentials, config and network")
		}
		for _, edition := range []string{"City", "ASN"} {
			if err := os.Rename(filepath.Join(stage, "GeoLite2-"+edition+".mmdb"), filepath.Join(stage, edition+".mmdb")); err != nil {
				return err
			}
		}
	} else {
		if err := u.downloadDBIP(ctx, stage, month); err != nil {
			return err
		}
	}
	if u.Provider == "maxmind" {
		fallback := filepath.Join(stage, "dbip")
		if err := os.Mkdir(fallback, 0755); err != nil {
			return err
		}
		if err := u.prepareFallback(ctx, fallback, current, month); err != nil {
			return err
		}
		fc, fa, err := geo.OpenPair(fallback, "dbip")
		if err != nil {
			return fmt.Errorf("fallback validation: %w", err)
		}
		fc.Close()
		fa.Close()
		if err := syncPair(fallback); err != nil {
			return err
		}
		if u.IPInfoToken != "" {
			path := filepath.Join(stage, "IPinfo.mmdb")
			if err := u.downloadIPInfo(ctx, path); err != nil {
				return err
			}
			info, err := geo.OpenIPInfo(path)
			if err != nil {
				return errors.New("IPinfo Lite database validation failed")
			}
			info.Close()
			f, err := os.OpenFile(path, os.O_RDWR, 0)
			if err != nil {
				return err
			}
			err = f.Sync()
			f.Close()
			if err != nil {
				return err
			}
		}
	}
	c, a, err := geo.OpenPair(stage, u.Provider)
	if err != nil {
		return fmt.Errorf("database validation failed: %w", err)
	}
	c.Close()
	a.Close()
	if err := syncPair(stage); err != nil {
		return err
	}
	files, err := u.tracking.collect(u.Provider, u.Provider == "maxmind", u.Provider == "maxmind" && u.IPInfoToken != "")
	if err != nil {
		return err
	}
	version := fmt.Sprintf("%s-%s-%d", u.Provider, month, time.Now().UnixNano())
	if err := os.Rename(stage, filepath.Join(u.Dir, version)); err != nil {
		return err
	}
	defer func() {
		if result != nil {
			u.discardUnpublished(version)
		}
	}()
	m := geo.Manifest{Version: version, Provider: u.Provider, UpdatedAt: time.Now().UTC(), Files: files, AuditRunID: runID}
	if u.Provider == "maxmind" {
		m.Hybrid = true
		m.DBIPMonth = month
		m.IPInfo = u.IPInfoToken != ""
	}
	if err := db.Prepare(runID, version, files); err != nil {
		return errors.New("cannot persist snapshot inventory")
	}
	if err := AtomicJSON(filepath.Join(u.Dir, "current.json"), m); err != nil {
		return err
	}
	status.Files = files
	publishedID = runID
	status.Snapshot = version
	status.AuditRunID = runID
	u.cleanup(version)
	return nil
}

// Reclaim a failed staged publication only when the serving manifest proves it
// is not active. AtomicJSON can rename successfully and then fail directory sync;
// in that case the snapshot must remain available even though Run returns error.
func (u *Updater) discardUnpublished(version string) {
	if !geo.ValidVersion(version) {
		return
	}
	b, err := os.ReadFile(filepath.Join(u.Dir, "current.json"))
	if err == nil {
		var current geo.Manifest
		if json.Unmarshal(b, &current) != nil || !geo.ValidVersion(current.Version) || current.Version == version {
			return
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		// An unreadable/ambiguous publication is not evidence it is unused.
		return
	}
	_ = os.RemoveAll(filepath.Join(u.Dir, version))
}

func (u *Updater) prepareFallback(ctx context.Context, destination string, current geo.Manifest, month string) error {
	if u.tracking != nil {
		return u.downloadDBIP(ctx, destination, month)
	}
	if current.Hybrid && current.DBIPMonth == month && current.Provider == "maxmind" && geo.ValidVersion(current.Version) {
		source := filepath.Join(u.Dir, current.Version, "dbip")
		c, a, err := geo.OpenPair(source, "dbip")
		if err == nil {
			c.Close()
			a.Close()
			for _, name := range []string{"City.mmdb", "ASN.mmdb"} {
				if err := copyFile(filepath.Join(source, name), filepath.Join(destination, name)); err != nil {
					return err
				}
			}
			return nil
		}
		// Missing/corrupt cached files trigger a fresh download, not an endless
		// retry of the same broken cache. The live snapshot is never modified.
	}
	return u.downloadDBIP(ctx, destination, month)
}

func (u *Updater) downloadIPInfo(ctx context.Context, path string) error {
	endpoint := "https://ipinfo.io/data/ipinfo_lite.mmdb?token=" + url.QueryEscape(u.IPInfoToken)
	return u.fetch(ctx, endpoint, path, false, 128<<20)
}

func (u *Updater) maxmindConfig() (string, func(), error) {
	noop := func() {}
	if u.ConfigFile != "" {
		if u.AccountID != "" || u.LicenseKey != "" {
			return "", noop, errors.New("conflicting MaxMind configuration")
		}
		return u.ConfigFile, noop, nil
	}
	id, err := strconv.ParseUint(u.AccountID, 10, 64)
	if err != nil || id == 0 || u.LicenseKey == "" || strings.ContainsAny(u.LicenseKey, " \t\r\n\x00") {
		return "", noop, errors.New("valid MAXMIND_ACCOUNT_ID and MAXMIND_LICENSE_KEY are required")
	}
	// Never write credentials into a database snapshot or command-line arguments.
	f, err := os.CreateTemp("", "myip-geoipupdate-*.conf")
	if err != nil {
		return "", noop, errors.New("cannot create temporary MaxMind configuration")
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	_, err = fmt.Fprintf(f, "AccountID %d\nLicenseKey %s\nEditionIDs GeoLite2-City GeoLite2-ASN\n", id, u.LicenseKey)
	ce := f.Close()
	if err != nil || ce != nil {
		cleanup()
		return "", noop, errors.New("cannot write temporary MaxMind configuration")
	}
	return f.Name(), cleanup, nil
}

func (u *Updater) downloadDBIP(ctx context.Context, dir, month string) error {
	for _, edition := range []string{"city", "asn"} {
		url := fmt.Sprintf("https://download.db-ip.com/free/dbip-%s-lite-%s.mmdb.gz", edition, month)
		name := "City.mmdb"
		if edition == "asn" {
			name = "ASN.mmdb"
		}
		if err := u.download(ctx, url, filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("DB-IP %s download: %w", edition, err)
		}
	}
	return nil
}

func syncPair(dir string) error {
	for _, name := range []string{"City.mmdb", "ASN.mmdb"} {
		if err := os.Chmod(filepath.Join(dir, name), 0644); err != nil {
			return err
		}
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_RDWR, 0)
		if err != nil {
			return err
		}
		err = f.Sync()
		f.Close()
		if err != nil {
			return err
		}
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = directory.Sync()
	directory.Close()
	if err != nil {
		return err
	}
	return nil
}
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	ce := out.Close()
	if err != nil {
		return err
	}
	return ce
}
func (u *Updater) download(ctx context.Context, url, path string) error {
	return u.fetch(ctx, url, path, true, 1<<30)
}
func (u *Updater) cleanup(current string) {
	entries, err := os.ReadDir(u.Dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == current {
			continue
		}
		if !strings.HasPrefix(e.Name(), "maxmind-") && !strings.HasPrefix(e.Name(), "dbip-") && !strings.HasPrefix(e.Name(), ".staging-") {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) < time.Hour {
			continue
		}
		_ = os.RemoveAll(filepath.Join(u.Dir, e.Name()))
	}
}
