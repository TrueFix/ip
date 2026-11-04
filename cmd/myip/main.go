package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/TrueFix/myip/internal/buildinfo"
	"github.com/TrueFix/myip/internal/geo"
	"github.com/TrueFix/myip/internal/httpapi"
)

func main() {
	version := flag.Bool("version", false, "print release version and exit")
	flag.Parse()
	if *version {
		fmt.Println(buildinfo.String())
		return
	}
	if err := run(); err != nil {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	c, err := httpapi.LoadConfig()
	if err != nil {
		return err
	}
	store := geo.NewStore(c.DataDir, c.Provider, geo.Options{ISPFromASN: c.ISPFromASN})
	defer store.Close()
	if err := store.Reload(); err != nil {
		slog.Warn("waiting for initial database snapshot")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	reloadDone := make(chan struct{})
	go func() {
		defer close(reloadDone)
		tick := time.NewTicker(30 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if err := store.Reload(); err != nil {
					slog.Warn("database reload failed; preserving current readers")
				}
			}
		}
	}()
	s := httpapi.New(c, store)
	newServer := func(addr string, h http.Handler) *http.Server {
		return &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	}
	servers := []*http.Server{newServer(c.Listen, s.Handler()), newServer(c.MetricsListen, s.MetricsHandler())}
	errs := make(chan error, len(servers))
	for _, server := range servers {
		go func() { slog.Info("listener starting", "address", server.Addr); errs <- server.ListenAndServe() }()
	}
	var result error
	select {
	case <-ctx.Done():
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			result = err
		}
	}
	cancel()
	shutdown, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	for _, server := range servers {
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}
	}
	<-reloadDone
	return result
}
