// Command scheduler is the one entry point: it loads configuration, opens the MySQL pool,
// builds the HTTP handler and runs the server (and, later, the workers) in one errgroup
// under a signal-cancelled context.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-kit/log"
	"golang.org/x/sync/errgroup"

	"scheduler/internal/app"
	"scheduler/internal/platform"
)

func main() {
	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	logger = log.With(logger, "ts", log.DefaultTimestampUTC, "caller", log.DefaultCaller)

	if err := run(logger); err != nil {
		_ = logger.Log("msg", "exit", "err", err)
		os.Exit(1)
	}
	_ = logger.Log("msg", "exit")
}

func run(logger log.Logger) error {
	cfg, err := platform.LoadConfig(os.Getenv)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	otelShutdown, err := platform.SetupOTel(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := otelShutdown(shutdownCtx); err != nil {
			_ = logger.Log("msg", "otel shutdown", "err", err)
		}
	}()

	db, err := platform.OpenMySQL(ctx, cfg.DSN)
	if err != nil {
		return err
	}
	defer db.Close()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           app.NewHandler(app.Deps{DB: db}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		_ = logger.Log("msg", "listening", "addr", cfg.Addr, "otel_exporter", cfg.OTelExporter)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	g.Go(func() error { // stop the listener when a signal arrives or any member of the group fails
		<-gctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	})
	// Workers join the group here: g.Go(relay.NewWorker(relaySvc, every).Run(gctx)).

	return g.Wait()
}
