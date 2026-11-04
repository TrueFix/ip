package updater

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func redirectResponse(location string) *http.Response {
	return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {location}}, Body: io.NopCloser(strings.NewReader(""))}
}

func databaseResponse() *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("database"))}
}

func TestDownloadRedirectDestinations(t *testing.T) {
	const token = "private-test-token"
	const ipinfo = "https://ipinfo.io/data/ipinfo_lite.mmdb?token=" + token
	const dbip = "https://download.db-ip.com/free/database.mmdb.gz"
	for _, tc := range []struct {
		name, endpoint, target string
		allowed                bool
	}{
		{"ipinfo relative", ipinfo, "/data/new.mmdb?token=" + token, true},
		{"ipinfo same host", ipinfo, "https://ipinfo.io/data/new.mmdb", true},
		{"ipinfo api", ipinfo, "https://api.ipinfo.io/data/new.mmdb", true},
		{"ipinfo R2 CDN", ipinfo, "https://dl.assets.ipinfo.io/data.mmdb?signature=cdn-only", true},
		{"ipinfo fallback CDN", ipinfo, "https://dl.ipinfo.io/data.mmdb?signature=cdn-only", true},
		{"ipinfo GCS", ipinfo, "https://storage.googleapis.com/bucket/data.mmdb?X-Goog-Signature=cdn-only", true},
		{"explicit HTTPS port", ipinfo, "https://dl.ipinfo.io:443/data.mmdb", true},
		{"case insensitive host", ipinfo, "https://DL.IPINFO.IO/data.mmdb", true},
		{"dbip relative", dbip, "/free/new.mmdb.gz", true},
		{"dbip same host", dbip, "https://download.db-ip.com/free/new.mmdb.gz", true},
		{"HTTP downgrade", ipinfo, "http://dl.ipinfo.io/data.mmdb", false},
		{"other scheme", ipinfo, "ftp://dl.ipinfo.io/data.mmdb", false},
		{"unrelated host", ipinfo, "https://attacker.example/data.mmdb", false},
		{"subdomain suffix", ipinfo, "https://dl.ipinfo.io.attacker.example/data.mmdb", false},
		{"unlisted subdomain", ipinfo, "https://other.ipinfo.io/data.mmdb", false},
		{"lookalike host", ipinfo, "https://dl-ipinfo.io/data.mmdb", false},
		{"IP literal", ipinfo, "https://127.0.0.1/data.mmdb", false},
		{"unexpected port", ipinfo, "https://dl.ipinfo.io:8443/data.mmdb", false},
		{"userinfo", ipinfo, "https://user:password@dl.ipinfo.io/data.mmdb", false},
		{"token in CDN query", ipinfo, "https://dl.ipinfo.io/data.mmdb?token=" + token, false},
		{"token in GCS query", ipinfo, "https://storage.googleapis.com/data.mmdb?token=" + token, false},
		{"escaped token", ipinfo, "https://dl.ipinfo.io/data.mmdb?token=%70rivate-test-token", false},
		{"token in CDN path", ipinfo, "https://dl.ipinfo.io/" + token + "/data.mmdb", false},
		{"DBIP cannot use IPinfo CDN", dbip, "https://dl.ipinfo.io/data.mmdb", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			u := Updater{IPInfoToken: token, Client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
				if !tc.allowed {
					t.Fatal("unsafe destination reached custom hook")
				}
				return nil
			}, Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					return redirectResponse(tc.target), nil
				}
				if !tc.allowed {
					t.Fatal("unsafe redirect reached transport")
				}
				if req.Header.Get("Referer") != "" {
					t.Fatal("redirect leaked Referer")
				}
				return databaseResponse(), nil
			})}}
			path := filepath.Join(t.TempDir(), "database.mmdb")
			err := u.fetch(context.Background(), tc.endpoint, path, false, 1024)
			if tc.allowed {
				if err != nil || calls != 2 {
					t.Fatalf("allowed redirect failed: calls=%d error=%v", calls, err)
				}
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "database" {
					t.Fatal("redirected download contents not written")
				}
			} else {
				if err == nil || calls != 1 {
					t.Fatalf("unsafe redirect accepted: calls=%d error=%v", calls, err)
				}
				if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), tc.target) {
					t.Fatal("redirect error exposes sensitive URL")
				}
			}
		})
	}
}

func TestDownloadRedirectPolicyPreservesClientAndHook(t *testing.T) {
	origin, _ := url.Parse("https://ipinfo.io/data/database.mmdb?token=private-test-token")
	hookCalls := 0
	hook := func(req *http.Request, via []*http.Request) error {
		hookCalls++
		if req.Header.Get("Referer") != "" {
			t.Fatal("hook received credential-bearing Referer")
		}
		req.Header.Set("X-Custom-Download", "retained")
		req.Header.Set("Referer", origin.String())
		return nil
	}
	transport := transportFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "ipinfo.io" {
			return redirectResponse("https://dl.ipinfo.io/database.mmdb?signature=cdn-only"), nil
		}
		if req.Header.Get("Referer") != "" || req.Header.Get("X-Custom-Download") != "retained" {
			t.Fatal("hook changes were not safely preserved")
		}
		return databaseResponse(), nil
	})
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	provided := &http.Client{Transport: transport, Jar: jar, Timeout: 123 * time.Second, CheckRedirect: hook}
	client := downloadClient(provided, origin, "private-test-token")
	if client == provided || client.Timeout != provided.Timeout || client.Jar != provided.Jar {
		t.Fatal("client was not cloned with original timeout")
	}
	response, err := client.Get(origin.String())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if hookCalls != 1 {
		t.Fatal("original hook was not invoked")
	}
	// Invoking the supplied client's hook directly still has its original
	// behavior, proving the caller's client was not overwritten.
	req, _ := http.NewRequest(http.MethodGet, "http://unrelated.example/", nil)
	if err := provided.CheckRedirect(req, nil); err != nil || req.Header.Get("Referer") == "" {
		t.Fatal("supplied client redirect policy changed")
	}
}

func TestDownloadRedirectHookCannotBypassPolicy(t *testing.T) {
	for _, tc := range []struct {
		name string
		hook func(*http.Request, []*http.Request) error
	}{
		{"scheme mutation", func(req *http.Request, _ []*http.Request) error { req.URL.Scheme = "http"; return nil }},
		{"host mutation", func(req *http.Request, _ []*http.Request) error { req.URL.Host = "attacker.example"; return nil }},
		{"authority mutation", func(req *http.Request, _ []*http.Request) error { req.Host = "attacker.example"; return nil }},
		{"origin mutation", func(req *http.Request, via []*http.Request) error {
			via[0].URL.Host = "attacker.example"
			req.URL.Host = "attacker.example"
			return nil
		}},
		{"token mutation", func(req *http.Request, _ []*http.Request) error {
			req.URL.RawQuery = "token=private-test-token"
			return nil
		}},
		{"hook veto", func(*http.Request, []*http.Request) error { return errors.New("private-test-token: signed-url-secret") }},
		{"stop redirects", func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			u := Updater{IPInfoToken: "private-test-token", Client: &http.Client{CheckRedirect: tc.hook, Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if calls > 1 {
					t.Fatal("unsafe or vetoed redirect reached transport")
				}
				return redirectResponse("https://dl.ipinfo.io/data.mmdb?signature=signed-url-secret"), nil
			})}}
			err := u.fetch(context.Background(), "https://ipinfo.io/data/test.mmdb?token=private-test-token", filepath.Join(t.TempDir(), "database"), false, 1024)
			if err == nil || calls != 1 {
				t.Fatal("hook decision not enforced")
			}
			if strings.Contains(err.Error(), "private-test-token") || strings.Contains(err.Error(), "signed-url-secret") {
				t.Fatal("custom redirect error leaked credentials")
			}
		})
	}
}

func TestDownloadRedirectLimitAcrossHosts(t *testing.T) {
	calls, hooks := 0, 0
	u := Updater{Client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { hooks++; return nil }, Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if calls > maxDownloadRedirects {
			t.Fatal("redirect limit bypassed by custom hook")
		}
		if req.Header.Get("Referer") != "" {
			t.Fatal("redirect Referer not stripped")
		}
		if req.URL.Host == "dl.ipinfo.io" {
			return redirectResponse("https://dl.assets.ipinfo.io/database.mmdb"), nil
		}
		return redirectResponse("https://dl.ipinfo.io/database.mmdb"), nil
	})}}
	if err := u.fetch(context.Background(), "https://ipinfo.io/data/test.mmdb", filepath.Join(t.TempDir(), "database"), false, 1024); err == nil {
		t.Fatal("redirect loop accepted")
	}
	if calls != maxDownloadRedirects || hooks != maxDownloadRedirects-1 {
		t.Fatalf("unexpected redirect limit: requests=%d hooks=%d", calls, hooks)
	}
}

func TestDownloadClientDefaults(t *testing.T) {
	origin, _ := url.Parse("https://download.db-ip.com/free/database.mmdb.gz")
	client := downloadClient(nil, origin, "")
	if client.Timeout != 10*time.Minute || client.CheckRedirect == nil {
		t.Fatal("default download timeout or redirect policy missing")
	}
}
