//go:build unix

package main

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Run the real lifecycle in a subprocess so signals and listener failures cannot
// interfere with the test runner's signal handlers or listeners.
func TestMyIPLifecycleHelper(t *testing.T) {
	if os.Getenv("MYIP_LIFECYCLE_HELPER") != "1" {
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

type serviceProcess struct {
	cmd    *exec.Cmd
	done   chan struct{}
	err    error
	output bytes.Buffer
}

func startService(t *testing.T, api, metrics string) *serviceProcess {
	t.Helper()
	p := &serviceProcess{done: make(chan struct{})}
	p.cmd = exec.Command(os.Args[0], "-test.run=^TestMyIPLifecycleHelper$")
	p.cmd.Env = []string{
		"MYIP_LIFECYCLE_HELPER=1", "GEOIP_PROVIDER=dbip",
		"DATA_DIR=" + t.TempDir(), "LISTEN_ADDR=" + api,
		"METRICS_ADDR=" + metrics,
	}
	p.cmd.Stdout, p.cmd.Stderr = &p.output, &p.output
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { p.err = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		select {
		case <-p.done:
		default:
			_ = p.cmd.Process.Kill()
			<-p.done
		}
	})
	return p
}

func freeAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func waitForHTTP(t *testing.T, p *serviceProcess, url string, status int) {
	t.Helper()
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-p.done:
			t.Fatalf("service exited before listening: %v\n%s", p.err, p.output.String())
		default:
		}
		r, err := client.Get(url)
		if err == nil {
			_ = r.Body.Close()
			if r.StatusCode == status {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for " + url)
}

func TestServiceSIGTERMBeforeInitialSnapshot(t *testing.T) {
	api, metrics := freeAddress(t), freeAddress(t)
	p := startService(t, api, metrics)
	waitForHTTP(t, p, "http://"+api+"/healthz", http.StatusServiceUnavailable)
	waitForHTTP(t, p, "http://"+metrics+"/metrics", http.StatusOK)
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.done:
		if p.err != nil {
			t.Fatalf("unclean shutdown: %v\n%s", p.err, p.output.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("SIGTERM did not stop service within grace period")
	}
	for _, addr := range []string{api, metrics} {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			t.Errorf("listener %s remains open", addr)
		}
	}
}

func TestListenerFailureStopsService(t *testing.T) {
	for _, occupied := range []string{"api", "metrics"} {
		t.Run(occupied, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			blocked, other := listener.Addr().String(), freeAddress(t)
			api, metrics := blocked, other
			if occupied == "metrics" {
				api, metrics = other, blocked
			}
			p := startService(t, api, metrics)
			select {
			case <-p.done:
				if p.err == nil || !strings.Contains(p.output.String(), "address already in use") {
					t.Fatalf("expected bind failure, got %v\n%s", p.err, p.output.String())
				}
			case <-time.After(10 * time.Second):
				t.Fatal("bind failure left service running")
			}
			conn, err := net.DialTimeout("tcp", other, 200*time.Millisecond)
			if err == nil {
				conn.Close()
				t.Fatal("other listener remains open after bind failure")
			}
		})
	}
}
