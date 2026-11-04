package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/TrueFix/myip/internal/audit"
	"github.com/TrueFix/myip/internal/buildinfo"
	"github.com/TrueFix/myip/internal/config"
	"github.com/TrueFix/myip/internal/updater"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	once := flag.Bool("once", false, "check for updates once, then exit")
	version := flag.Bool("version", false, "print release version and exit")
	status := flag.Bool("status", false, "print recent SQLite update history and exit (no credentials needed)")
	flag.Parse()
	if *version {
		fmt.Println(buildinfo.String())
		return
	}
	if *status {
		r, err := config.LoadRuntime()
		if err != nil {
			slog.Error("invalid configuration", "error", err)
			os.Exit(1)
		}
		runs, err := audit.History(r.DataDir)
		if err != nil {
			slog.Error("cannot read SQLite update history")
			os.Exit(1)
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(runs); err != nil {
			os.Exit(1)
		}
		return
	}
	c, err := config.LoadUpdater()
	if err != nil {
		slog.Error("invalid updater configuration", "error", err)
		os.Exit(1)
	}
	u := updater.Updater{Dir: c.DataDir, Provider: c.Provider, ConfigFile: c.ConfigFile, AccountID: c.AccountID, LicenseKey: c.LicenseKey, IPInfoToken: c.IPInfoToken}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	for {
		attempt, cancel := context.WithTimeout(ctx, 30*time.Minute)
		err := u.Run(attempt)
		cancel()
		if err != nil {
			slog.Error("database update failed", "error", err)
		} else {
			slog.Info("database update check completed")
		}
		if *once {
			if err != nil {
				os.Exit(1)
			}
			return
		}
		delay := c.Interval
		if err != nil {
			delay = 15 * time.Minute
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
