package updater

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxDownloadRedirects = 10

// downloadClient clones the caller's client: a shared client must not acquire
// updater-specific policy. Transport, timeout, cookie jar and custom redirect
// decisions are retained, but hooks cannot bypass the download safety checks.
func downloadClient(provided *http.Client, origin *url.URL, token string) *http.Client {
	client := http.Client{Timeout: 10 * time.Minute}
	if provided != nil {
		client = *provided
	}
	// Snapshot the origin before invoking any caller hook.
	original := *origin
	hook := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		req.Header.Del("Referer")
		if len(via) >= maxDownloadRedirects {
			return errors.New("database download redirect limit exceeded")
		}
		if err := safeDownloadRedirect(req, &original, token); err != nil {
			return err
		}
		if hook != nil {
			if err := hook(req, via); err != nil {
				return err
			}
		}
		// A custom hook can mutate the URL or reintroduce a credential-bearing
		// Referer. Check again before net/http sends the redirected request.
		req.Header.Del("Referer")
		return safeDownloadRedirect(req, &original, token)
	}
	return &client
}

func safeDownloadRedirect(req *http.Request, origin *url.URL, token string) error {
	u := req.URL
	if u == nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") || (req.Host != "" && !strings.EqualFold(req.Host, u.Host)) {
		return errors.New("unsafe database download redirect")
	}
	host := strings.ToLower(u.Hostname())
	originalHost := strings.ToLower(origin.Hostname())
	// DB-IP publishes download.db-ip.com URLs at
	// https://db-ip.com/db/download/ip-to-city-lite. Its downloads need no
	// documented cross-host exception; future CDN changes fail closed.
	allowed := host == originalHost
	if originalHost == "ipinfo.io" {
		// Exact hosts documented at https://ipinfo.io/developers/database-download
		// (How downloads are delivered). Do not allow arbitrary subdomains or
		// arbitrary cloud-storage hosts: CDN signed URLs must not carry the
		// account token, including on the shared Google storage host.
		switch host {
		case "api.ipinfo.io", "dl.assets.ipinfo.io", "dl.ipinfo.io", "storage.googleapis.com":
			allowed = true
		}
	}
	if !allowed {
		return errors.New("unapproved database download redirect host")
	}
	if host != originalHost && token != "" {
		// Check decoded paths/queries as well as raw URLs so escaping cannot
		// accidentally forward the account credential to a CDN. Other signed
		// URL parameters remain untouched.
		decoded, err := url.QueryUnescape(u.String())
		if err != nil || strings.Contains(decoded, token) || strings.Contains(u.String(), token) {
			return errors.New("credential-bearing database download redirect")
		}
	}
	return nil
}
