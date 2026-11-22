package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestSQLRedaction(t *testing.T) {
	for _, query := range []string{`SELECT 'secret@example.com', 12345 -- password=secret`, `SELECT $tag$secret@example.com$tag$`, `UPDATE users SET password='secret@example.com'`} {
		masked := MaskSQL(query)
		if strings.Contains(masked, "secret") || strings.Contains(masked, "12345") {
			t.Fatalf("leaked value: %s", masked)
		}
	}
}

type statusWriter struct{ codes []int }

func (w *statusWriter) Header() http.Header         { return http.Header{} }
func (w *statusWriter) WriteHeader(code int)        { w.codes = append(w.codes, code) }
func (w *statusWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestHTTPInstrumentationHandlesImplicitAndInterimStatus(t *testing.T) {
	previous := slog.Default()
	defer slog.SetDefault(previous)
	var output bytes.Buffer
	slog.SetDefault(JSONLogger(&output))
	HTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/empty", nil))
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["status"] != "OK" {
		t.Fatalf("implicit status: %v", record["status"])
	}
	underlying := &statusWriter{}
	w := &responseWriter{ResponseWriter: underlying}
	w.WriteHeader(http.StatusEarlyHints)
	w.WriteHeader(http.StatusOK)
	w.WriteHeader(http.StatusInternalServerError)
	if w.status != 200 || !slices.Equal(underlying.codes, []int{103, 200}) {
		t.Fatalf("status=%d writes=%v", w.status, underlying.codes)
	}
}

func TestHTTPRequestLogRetainsAuthenticatedActor(t *testing.T) {
	previous := slog.Default()
	defer slog.SetDefault(previous)
	var output bytes.Buffer
	slog.SetDefault(JSONLogger(&output))
	handler := HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(WithFields(r.Context(), "actor_id", 42, "actor_name", "editor", "target_id", 7, "database", "sales"))
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/test", nil))
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["actor_id"] != float64(42) || record["target_id"] != float64(7) {
		t.Fatalf("request lost authenticated fields: %v", record)
	}
}

func TestTracePropagationAndJSON(t *testing.T) {
	parent := "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"
	ctx := Extract(context.Background(), parent)
	if TraceParent(ctx) != parent {
		t.Fatal("trace context lost")
	}
	var b bytes.Buffer
	JSONLogger(&b).InfoContext(WithFields(ctx, "actor_id", 42), "test", "event", "test", "status", "SUCCESS")
	var data map[string]any
	if err := json.Unmarshal(b.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data["trace_id"] != trace.SpanContextFromContext(ctx).TraceID().String() || data["actor_id"] != float64(42) {
		t.Fatal(data)
	}
}
