//go:build unix

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

func TestUpdaterLifecycleHelper(t *testing.T) {
	if os.Getenv("MYIP_UPDATER_HELPER") != "1" {
		return
	}
	os.Args = []string{"updater"}
	if os.Getenv("MYIP_UPDATER_ONCE") == "1" {
		os.Args = append(os.Args, "--once")
	}
	main()
	os.Exit(0)
}

func TestUpdaterLockFailureLifecyclePreservesManifest(t *testing.T) {
	for _, once := range []bool{false, true} {
		name := "retry-SIGTERM"
		if once {
			name = "once-failure"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			manifest := []byte(`{"provider":"dbip","version":"existing-snapshot"}`)
			path := filepath.Join(dir, "current.json")
			if err := os.WriteFile(path, manifest, 0644); err != nil {
				t.Fatal(err)
			}
			lock := flock.New(filepath.Join(dir, ".updater.lock"))
			if err := lock.Lock(); err != nil {
				t.Fatal(err)
			}
			defer lock.Close()

			// The held writer lock exercises a deterministic failure/retry without
			// credentials, provider calls, or modifying the existing snapshot.
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestUpdaterLifecycleHelper$")
			cmd.Env = []string{"MYIP_UPDATER_HELPER=1", "GEOIP_PROVIDER=dbip", "DATA_DIR=" + dir}
			if once {
				cmd.Env = append(cmd.Env, "MYIP_UPDATER_ONCE=1")
			}
			logPath := filepath.Join(t.TempDir(), "output.log")
			log, err := os.Create(logPath)
			if err != nil {
				t.Fatal(err)
			}
			defer log.Close()
			cmd.Stdout, cmd.Stderr = log, log
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			var waitErr error
			go func() { waitErr = cmd.Wait(); close(done) }()
			defer func() { cancel(); <-done }()
			if !once {
				deadline := time.Now().Add(10 * time.Second)
				for {
					output, err := os.ReadFile(logPath)
					if err != nil {
						t.Fatal(err)
					}
					if bytes.Contains(output, []byte("another updater already holds")) {
						break
					}
					select {
					case <-done:
						t.Fatalf("updater stopped before retry: %v\n%s", waitErr, output)
					default:
					}
					if time.Now().After(deadline) {
						t.Fatal("updater never reported lock contention")
					}
					time.Sleep(10 * time.Millisecond)
				}
				if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-done:
				if once && waitErr == nil {
					t.Fatal("--once must fail on lock contention")
				}
				if !once && waitErr != nil {
					t.Fatalf("SIGTERM shutdown failed: %v", waitErr)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("updater did not stop within grace period")
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, manifest) {
				t.Fatalf("active manifest changed: %v", err)
			}
		})
	}
}
