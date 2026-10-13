// Command scheduler is the one entry point: it loads configuration, opens the MySQL pool,
// wires the domains, builds the HTTP handler and runs the server and the no-show sweeper in one
// errgroup under a signal-cancelled context.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-kit/log"
	"go.opentelemetry.io/otel"
	"golang.org/x/sync/errgroup"

	"scheduler/internal/app"
	customersmysql "scheduler/internal/customers/mysql"
	"scheduler/internal/platform"
	schedendpoint "scheduler/internal/scheduling/endpoint"
	schedmysql "scheduler/internal/scheduling/mysql"
	schedsvc "scheduler/internal/scheduling/service"
	"scheduler/internal/scheduling/sweeper"
	setupmysql "scheduler/internal/setup/mysql"
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

	store := schedmysql.New(db)
	schedulingSvc := newSchedulingService(db, store, logger)
	handler := app.NewHandler(app.Deps{
		DB:         db,
		Scheduling: schedendpoint.Specs(schedulingSvc),
	})
	srv := &http.Server{Addr: cfg.Addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}

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
	// The no-show sweeper marks overdue appointments through the same wrapped service the HTTP
	// handlers use. It stops with the context, which is a normal exit.
	sweep := sweeper.NewWorker(sweeper.NewService(store, schedulingSvc, platform.RealClock{}, cfg.NoShowGrace,
		log.With(logger, "component", "sweeper")), cfg.NoShowSweepInterval)
	g.Go(func() error {
		if err := sweep.Run(gctx)(); !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	})

	return g.Wait()
}

// newSchedulingService wires the Scheduling service: its store, the setup and customers
// repositories as its readers, the real clock and ids, and its middleware, outermost last so
// tracing runs first.
func newSchedulingService(db *sql.DB, store *schedmysql.Store, logger log.Logger) schedsvc.Service {
	svc := schedsvc.NewService(store, setupmysql.New(db), customersmysql.New(db), platform.RealClock{}, platform.UUIDs{})
	svc = schedsvc.LoggingMiddleware(log.With(logger, "component", "scheduling"))(svc)
	svc = schedsvc.InstrumentingMiddleware(otel.Meter("scheduler"))(svc)
	return schedsvc.TracingMiddleware(otel.Tracer("scheduler"))(svc)
}
