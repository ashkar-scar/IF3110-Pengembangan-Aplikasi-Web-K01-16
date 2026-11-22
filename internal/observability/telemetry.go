package observability

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type fieldsKey struct{}
type requestKey struct{}
type requestFields struct {
	mu     sync.Mutex
	values map[string]any
}

func (f *requestFields) update(fields []any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := 0; i+1 < len(fields); i += 2 {
		if key, ok := fields[i].(string); ok {
			switch key {
			case "actor_id", "actor_name", "target_id", "database":
				f.values[key] = fields[i+1]
			}
		}
	}
}
func (f *requestFields) snapshot() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := []any{}
	for _, key := range []string{"actor_id", "actor_name", "target_id", "database"} {
		if value, ok := f.values[key]; ok {
			result = append(result, key, value)
		}
	}
	return result
}

func WithFields(ctx context.Context, fields ...any) context.Context {
	if state, ok := ctx.Value(requestKey{}).(*requestFields); ok {
		state.update(fields)
	}
	old, _ := ctx.Value(fieldsKey{}).([]any)
	return context.WithValue(ctx, fieldsKey{}, append(append([]any{}, old...), fields...))
}

func Event(ctx context.Context, event, status string, fields ...any) {
	args := []any{"event", event, "status", status}
	args = append(args, fields...)
	slog.InfoContext(ctx, event, args...)
}

type contextHandler struct {
	json   slog.Handler
	remote slog.Handler
}

func (h contextHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.json.Enabled(ctx, l)
}
func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	attrs := []slog.Attr{}
	positions := map[string]int{}
	add := func(a slog.Attr) {
		if i, ok := positions[a.Key]; ok {
			attrs[i] = a
		} else {
			positions[a.Key] = len(attrs)
			attrs = append(attrs, a)
		}
	}
	r.Attrs(func(a slog.Attr) bool { add(a); return true })
	_, hasEvent := positions["event"]
	if !hasEvent {
		r.Message = strings.SplitN(r.Message, ": ", 2)[0]
	}
	fields, _ := ctx.Value(fieldsKey{}).([]any)
	if state, ok := ctx.Value(requestKey{}).(*requestFields); ok {
		root := state.snapshot()
		for i := 0; i+1 < len(root); i += 2 {
			add(slog.Any(root[i].(string), root[i+1]))
		}
	}
	for i := 0; i+1 < len(fields); i += 2 {
		if key, ok := fields[i].(string); ok {
			add(slog.Any(key, fields[i+1]))
		}
	}
	for _, a := range []slog.Attr{slog.String("event", "application"), slog.String("status", r.Level.String()), slog.Int("actor_id", 0), slog.String("actor_name", ""), slog.Int("target_id", 0), slog.String("database", "")} {
		if _, ok := positions[a.Key]; !ok {
			add(a)
		}
	}
	sc := trace.SpanContextFromContext(ctx)
	add(slog.String("trace_id", sc.TraceID().String()))
	add(slog.String("span_id", sc.SpanID().String()))
	add(slog.String("correlation_id", sc.TraceID().String()))
	normalized := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	normalized.AddAttrs(attrs...)
	err := h.json.Handle(ctx, normalized)
	if h.remote != nil {
		err = errors.Join(err, h.remote.Handle(ctx, normalized))
	}
	return err
}
func (h contextHandler) WithAttrs(a []slog.Attr) slog.Handler {
	h.json = h.json.WithAttrs(a)
	if h.remote != nil {
		h.remote = h.remote.WithAttrs(a)
	}
	return h
}
func (h contextHandler) WithGroup(n string) slog.Handler {
	h.json = h.json.WithGroup(n)
	if h.remote != nil {
		h.remote = h.remote.WithGroup(n)
	}
	return h
}

func Init(ctx context.Context, service string) (func(context.Context) error, error) {
	res := resource.NewSchemaless(attribute.String("service.name", service))
	var exporter sdktrace.SpanExporter
	var err error
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		exporter, err = otlptracehttp.New(ctx)
	} else {
		exporter, err = stdouttrace.New(stdouttrace.WithWriter(os.Stdout))
	}
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(res))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	handler := contextHandler{json: slog.NewJSONHandler(os.Stdout, nil)}
	var logs *sdklog.LoggerProvider
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		logExporter, e := otlploghttp.New(ctx)
		if e != nil {
			provider.Shutdown(ctx)
			return nil, e
		}
		logs = sdklog.NewLoggerProvider(sdklog.WithResource(res), sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)))
		handler.remote = otelslog.NewHandler(service, otelslog.WithLoggerProvider(logs))
	}
	slog.SetDefault(slog.New(handler))
	return func(ctx context.Context) error {
		var e error
		if logs != nil {
			e = logs.Shutdown(ctx)
		}
		return errors.Join(e, provider.Shutdown(ctx))
	}, nil
}

func JSONLogger(w io.Writer) *slog.Logger {
	return slog.New(contextHandler{json: slog.NewJSONHandler(w, nil)})
}
func TraceParent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	return carrier.Get("traceparent")
}
func Extract(ctx context.Context, parent string) context.Context {
	return propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{"traceparent": parent})
}
func Start(ctx context.Context, name string) (context.Context, trace.Span) {
	return otel.Tracer("ruangdb").Start(ctx, name)
}
func End(span trace.Span, err error) {
	if err != nil {
		span.SetStatus(codes.Error, SafeError(err))
		span.SetAttributes(attribute.String("error.type", SQLState(err)))
	}
	span.End()
}
func SQLState(err error) string {
	var state interface{ SQLState() string }
	if errors.As(err, &state) {
		return state.SQLState()
	}
	return ""
}
func SafeError(err error) string {
	if err == nil {
		return ""
	}
	if state := SQLState(err); state != "" {
		return "SQL gagal (SQLSTATE " + state + ")"
	}
	if errors.Is(err, context.Canceled) {
		return "operasi dibatalkan"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "batas waktu operasi terlampaui"
	}
	return "operasi gagal; periksa log korelasi dan konektivitas"
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(code int) {
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		w.ResponseWriter.WriteHeader(code)
		return
	}
	if w.status == 0 {
		w.status = code
		w.ResponseWriter.WriteHeader(code)
	}
}
func (w *responseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func HTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		ctx := propagation.TraceContext{}.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx = context.WithValue(ctx, requestKey{}, &requestFields{values: map[string]any{}})
		ctx, span := Start(ctx, "api.request")
		defer span.End()
		started := time.Now()
		rw := &responseWriter{ResponseWriter: w}
		request := r.WithContext(ctx)
		next.ServeHTTP(rw, request)
		if rw.status == 0 {
			rw.status = http.StatusOK
		}
		pattern := request.Pattern
		if pattern == "" {
			pattern = r.Method + " /unmatched"
		}
		span.SetAttributes(attribute.String("http.route", pattern), attribute.Int("http.response.status_code", rw.status))
		if rw.status >= 500 {
			span.SetStatus(codes.Error, http.StatusText(rw.status))
		}
		Event(ctx, "http_request", http.StatusText(rw.status), "method", r.Method, "route", strings.TrimSpace(pattern), "duration_ms", time.Since(started).Milliseconds())
	})
}
