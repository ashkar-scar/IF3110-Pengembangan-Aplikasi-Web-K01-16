package main

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/observability"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/queue"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/worker"
)

func run(ctx context.Context) error {
	shutdown, err := observability.Init(ctx, "ruangdb-worker")
	if err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdown(ctx)
	}()
	pool, err := store.NewPostgresPool(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	if err = store.SetupPlatform(ctx, pool); err != nil {
		return err
	}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("TARGET_CREDENTIAL_KEY"))
	if err != nil {
		return err
	}
	targets, err := store.NewTargetRepository(pool, key)
	if err != nil {
		return err
	}
	metrics := observability.NewMetrics(pool)
	w := worker.New(queue.NewRepository(pool), targets, metrics)
	if raw := os.Getenv("WORKER_CONCURRENCY"); raw != "" {
		w.Concurrency, err = strconv.Atoi(raw)
		if err != nil {
			return err
		}
	}
	address := os.Getenv("WORKER_METRICS_ADDR")
	if address == "" {
		address = "127.0.0.1:9091"
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metrics.Handler())
	server := &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	failures := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			failures <- err
		}
	}()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	workerDone := make(chan error, 1)
	go func() { workerDone <- w.Run(runCtx) }()
	select {
	case err = <-failures:
		cancel()
		<-workerDone
	case err = <-workerDone:
		cancel()
	}
	stopCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	server.Shutdown(stopCtx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx); err != nil {
		slog.Error("worker berhenti", "error", observability.SafeError(err))
		os.Exit(1)
	}
}
