package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/observability"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/queue"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/targetdb"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/worker"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type result struct {
	Rows          int       `json:"rows"`
	NormalSeconds float64   `json:"normal_update_seconds"`
	BatchSeconds  float64   `json:"batch_seconds"`
	BatchSize     int       `json:"batch_size"`
	Batches       int       `json:"committed_batches"`
	RowsProcessed int64     `json:"rows_processed"`
	PostgreSQL    string    `json:"postgresql"`
	TestedAt      time.Time `json:"tested_at"`
}

func benchmark(ctx context.Context) (result, error) {
	var report result
	dsn := os.Getenv("BENCHMARK_DSN")
	if dsn == "" {
		return report, errors.New("set BENCHMARK_DSN ke PostgreSQL pengujian dengan izin CREATEDB")
	}
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return report, err
	}
	defer admin.Close(context.Background())
	name := "track3_benchmark_" + uuid.NewString()[:8]
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+quoted+" TEMPLATE template0"); err != nil {
		return report, err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		admin.Exec(cleanup, "DROP DATABASE "+quoted+" WITH (FORCE)")
	}()
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return report, err
	}
	config.Database = name
	target, err := targetdb.OpenPostgres(ctx, targetdb.Config{Host: config.Host, Port: int(config.Port), Database: name, Username: config.User, SSLMode: "disable"}, config.Password)
	if err != nil {
		return report, err
	}
	defer target.Close()
	if _, err = target.DB.ExecContext(ctx, `CREATE TABLE public.customers(id bigint PRIMARY KEY,tier text NOT NULL,padding text NOT NULL)`); err != nil {
		return report, err
	}
	rows := 100000
	for {
		if _, err = target.DB.ExecContext(ctx, `TRUNCATE public.customers`); err != nil {
			return report, err
		}
		if _, err = target.DB.ExecContext(ctx, `INSERT INTO public.customers SELECT i,'SILVER',repeat('x',128) FROM generate_series(1,$1::int) i`, rows); err != nil {
			return report, err
		}
		tx, err := target.DB.BeginTx(ctx, nil)
		if err != nil {
			return report, err
		}
		start := time.Now()
		_, err = tx.ExecContext(ctx, `UPDATE public.customers SET tier='GOLD'`)
		duration := time.Since(start)
		rollbackErr := tx.Rollback()
		if err != nil {
			return report, err
		}
		if rollbackErr != nil {
			return report, rollbackErr
		}
		if duration > time.Second {
			report.Rows = rows
			report.NormalSeconds = duration.Seconds()
			break
		}
		rows *= 2
		if rows > 3200000 {
			return report, errors.New("UPDATE belum >1 detik sampai batas dataset; jalankan pada lingkungan pengujian yang sesuai")
		}
	}
	platformConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return report, err
	}
	platformConfig.ConnConfig.Database = name
	platformConfig.ConnConfig.RuntimeParams["search_path"] = "platform"
	if _, err = target.DB.ExecContext(ctx, `CREATE SCHEMA platform`); err != nil {
		return report, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, platformConfig)
	if err != nil {
		return report, err
	}
	defer pool.Close()
	if err = store.SetupPlatform(ctx, pool); err != nil {
		return report, err
	}
	var actor, group int64
	if err = pool.QueryRow(ctx, `INSERT INTO users(username,password_hash) VALUES('benchmark','unused') RETURNING id`).Scan(&actor); err != nil {
		return report, err
	}
	if err = pool.QueryRow(ctx, `INSERT INTO groups(name,leader_id) VALUES('benchmark',$1) RETURNING id`, actor).Scan(&group); err != nil {
		return report, err
	}
	targets, err := store.NewTargetRepository(pool, make([]byte, 32))
	if err != nil {
		return report, err
	}
	record, err := targets.Create(ctx, core.DatabaseTarget{Name: "benchmark", Driver: "postgres", Host: config.Host, Port: int(config.Port), Database: name, Username: config.User, Password: config.Password, SSLMode: "disable", Active: true})
	if err != nil {
		return report, err
	}
	if _, err = pool.Exec(ctx, `INSERT INTO group_members VALUES($1,$2)`, group, actor); err != nil {
		return report, err
	}
	if _, err = pool.Exec(ctx, `INSERT INTO database_grants VALUES($1,$2,'EDIT')`, group, record.ID); err != nil {
		return report, err
	}
	migrations := store.NewMigrationRepository(pool)
	report.BatchSize = 5000
	draft, err := migrations.CreateDraft(ctx, core.Draft{TargetID: record.ID, Description: "Benchmark batch", SQLUp: `UPDATE public.customers SET tier='GOLD' WHERE id IN (SELECT id FROM public.customers WHERE id > $1 ORDER BY id LIMIT $2)`, SQLDown: `UPDATE public.customers SET tier='SILVER' WHERE id IN (SELECT id FROM public.customers WHERE id > $1 AND id <= $2 ORDER BY id LIMIT $3)`, ExecutionMode: core.ExecutionBatch, BatchColumn: "id", BatchSize: report.BatchSize, CreatedBy: actor})
	if err != nil {
		return report, err
	}
	request, err := migrations.SubmitReview(ctx, record.ID, draft.ID, actor)
	if err != nil {
		return report, err
	}
	if _, err = migrations.ApproveReview(ctx, record.ID, request.ID, actor, ""); err != nil {
		return report, err
	}
	service := queue.NewService(queue.NewRepository(pool))
	jobID, err := service.Enqueue(ctx, queue.EnqueueRequest{RequestID: request.ID, TargetID: record.ID, ActorID: actor, Action: queue.ActionUp})
	if err != nil {
		return report, err
	}
	w := worker.New(service.Repository, targets, nil)
	start := time.Now()
	_, err = w.RunOne(ctx)
	if err != nil {
		return report, err
	}
	report.BatchSeconds = time.Since(start).Seconds()
	job, err := service.Get(ctx, jobID)
	if err != nil {
		return report, err
	}
	if job.Status != "SUCCESS" || job.RowsProcessed != int64(report.Rows) {
		return report, fmt.Errorf("hasil batch tidak sesuai: %s, %d rows", job.Status, job.RowsProcessed)
	}
	report.Batches = job.CompletedBatches
	report.RowsProcessed = job.RowsProcessed
	report.TestedAt = time.Now().UTC()
	target.DB.QueryRowContext(ctx, "SHOW server_version").Scan(&report.PostgreSQL)
	return report, nil
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		shutdown, err := observability.Init(ctx, "ruangdb-worker")
		if err != nil {
			fmt.Fprintln(os.Stderr, "telemetry initialization failed")
			os.Exit(1)
		}
		defer func() {
			flush, stop := context.WithTimeout(context.Background(), 10*time.Second)
			defer stop()
			shutdown(flush)
		}()
	}
	report, err := benchmark(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	json.NewEncoder(os.Stdout).Encode(report)
}
