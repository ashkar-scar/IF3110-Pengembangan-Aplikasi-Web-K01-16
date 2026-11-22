package worker_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/api"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/auth"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/core"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/observability"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/store"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestAPIQueueWorkerTraceAndMetrics(t *testing.T) {
	f := setup(t)
	oldProvider := otel.GetTracerProvider()
	defer otel.SetTracerProvider(oldProvider)
	recorder := tracetest.NewSpanRecorder()
	var shutdown func(context.Context) error
	remote := os.Getenv("TRACK3_OBSERVABILITY_TEST") == "1"
	if remote {
		var err error
		shutdown, err = observability.Init(f.ctx, "ruangdb-worker")
		if err != nil {
			t.Fatal(err)
		}
		otel.GetTracerProvider().(*sdktrace.TracerProvider).RegisterSpanProcessor(recorder)
	} else {
		provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
		otel.SetTracerProvider(provider)
		shutdown = provider.Shutdown
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdown(ctx)
	}()
	hash, err := auth.CreatePasswordHash("integration-password")
	if err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE users SET password_hash=$2 WHERE id=$1`, f.actor, hash)
	app := api.Create(auth.NewService(store.NewUserRepository(f.pool), store.NewSessionRepository(f.pool)), false)
	app.UseTargets(api.NewRegistryTargets(f.worker.Targets, store.NewPermissionRepository(f.pool)), t.TempDir())
	app.UseMigrations(f.migrations)
	app.UseJobs(f.service)
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", f.worker.Metrics.Handler())
	mux.Handle("/", app.Router())
	server := httptest.NewServer(observability.HTTP(mux))
	defer server.Close()
	if remote {
		listener, err := net.Listen("tcp", ":9091")
		if err != nil {
			t.Fatal(err)
		}
		metricServer := &http.Server{Handler: f.worker.Metrics.Handler(), ReadHeaderTimeout: time.Second}
		go metricServer.Serve(listener)
		defer metricServer.Close()
	}
	response, err := http.Post(server.URL+"/api/auth/login", "application/json", strings.NewReader(`{"username":"worker-editor","password":"integration-password"}`))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		t.Fatalf("login: %d", response.StatusCode)
	}
	var login struct {
		CSRF string `json:"csrf_token"`
	}
	json.NewDecoder(response.Body).Decode(&login)
	response.Body.Close()
	cookies := response.Cookies()
	q := f.request("CREATE TABLE telemetry_test(id bigint primary key)", "DROP TABLE telemetry_test", core.ExecutionNormal)
	request, err := http.NewRequest("POST", fmt.Sprintf("%s/api/databases/%d/reviews/%d/enqueue", server.URL, f.targetID, q.ID), bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	request.Header.Set("X-CSRF-Token", login.CSRF)
	request.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 202 {
		t.Fatalf("enqueue: %d", response.StatusCode)
	}
	var enqueued struct {
		ID string `json:"job_id"`
	}
	json.NewDecoder(response.Body).Decode(&enqueued)
	response.Body.Close()
	f.run(enqueued.ID, "SUCCESS")
	job, err := f.service.Get(f.ctx, enqueued.ID)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(job.TraceContext, "-")
	if len(parts) != 4 {
		t.Fatal("queue lost trace context")
	}
	traceID := parts[1]
	found := map[string]bool{}
	for _, span := range recorder.Ended() {
		if span.SpanContext().TraceID().String() == traceID {
			found[span.Name()] = true
		}
	}
	for _, name := range []string{"api.request", "worker.process", "lock.acquire", "db.query"} {
		if !found[name] {
			t.Fatalf("missing correlated span %s", name)
		}
	}
	metricResponse := httptest.NewRecorder()
	f.worker.Metrics.Handler().ServeHTTP(metricResponse, httptest.NewRequest("GET", "/metrics", nil))
	if metricResponse.Code != 200 || !strings.Contains(metricResponse.Body.String(), "ruangdb_jobs") {
		t.Fatal("metrics unavailable")
	}
	if remote {
		ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
		err = shutdown(ctx)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		client := &http.Client{Timeout: 2 * time.Second}
		endpoints := map[string]string{
			"trace":   "http://127.0.0.1:3000/api/datasources/proxy/uid/traces/api/traces/" + traceID,
			"logs":    "http://127.0.0.1:3000/api/datasources/proxy/uid/logs/loki/api/v1/query_range?query=" + url.QueryEscape(`{service_name="ruangdb-worker"} | trace_id="`+traceID+`"`),
			"metrics": "http://127.0.0.1:9090/api/v1/query?query=" + url.QueryEscape(`ruangdb_jobs{job="ruangdb-worker",status="SUCCESS"}>0`),
		}
		for kind, endpoint := range endpoints {
			passed := false
			for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
				r, err := client.Get(endpoint)
				if err == nil {
					var data map[string]any
					decodeErr := json.NewDecoder(r.Body).Decode(&data)
					r.Body.Close()
					if r.StatusCode == 200 && decodeErr == nil {
						if kind == "trace" {
							_, passed = data["batches"]
							if !passed {
								_, passed = data["resourceSpans"]
							}
						}
						if kind != "trace" {
							if d, ok := data["data"].(map[string]any); ok {
								if records, ok := d["result"].([]any); ok {
									passed = len(records) > 0
								}
							}
						}
					}
				}
				if passed {
					break
				}
				time.Sleep(500 * time.Millisecond)
			}
			if !passed {
				t.Fatalf("observability backend %s did not receive evidence", kind)
			}
		}
		if path := os.Getenv("TRACK3_EVIDENCE_PATH"); path != "" {
			data, _ := json.MarshalIndent(map[string]any{"trace_id": traceID, "api_status": 202, "job_status": job.Status, "tempo": true, "loki": true, "prometheus": true, "tested_at": time.Now().UTC()}, "", "  ")
			if err = os.WriteFile(path, append(data, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Logf("trace_id=%s; API 202 -> queue -> worker SUCCESS; metrics available", traceID)
}
