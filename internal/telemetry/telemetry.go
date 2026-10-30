package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/VictoriaMetrics/metrics"
)

type contextKey string

const (
	TraceIDKey contextKey = "trace_id"
	SpanIDKey  contextKey = "span_id"
)

var (
	RequestsTotal = func(method, path string, status int) *metrics.Counter {
		return metrics.GetOrCreateCounter(fmt.Sprintf(`http_requests_total{method="%s",path="%s",status="%d"}`, method, path, status))
	}
	RequestDuration = func(path string) *metrics.Histogram {
		return metrics.GetOrCreateHistogram(fmt.Sprintf(`http_request_duration_seconds{path="%s"}`, path))
	}
	InFlightRequests = metrics.NewGauge("http_requests_in_flight", nil)
	Logger           *slog.Logger
)

func init() {
	metrics.ExposeMetadata(true)
	Logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

type statusWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		traceID, spanID := parseOrCreateTrace(r.Header.Get("traceparent"))
		ctx := context.WithValue(r.Context(), TraceIDKey, traceID)
		ctx = context.WithValue(ctx, SpanIDKey, spanID)
		r = r.WithContext(ctx)

		w.Header().Set("X-Trace-Id", traceID)

		InFlightRequests.Inc()
		defer InFlightRequests.Dec()

		sw := &statusWriter{ResponseWriter: w, statusCode: http.StatusOK}


		next.ServeHTTP(sw, r)

		duration := time.Since(start)

		if r.URL.Path != "/metrics" {

			RequestsTotal(r.Method, r.URL.Path, sw.statusCode).Inc()
			RequestDuration(r.URL.Path).Update(duration.Seconds())


			Logger.Info("http_request_completed",
				slog.String("trace_id", traceID),
				slog.String("span_id", spanID),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", sw.statusCode),
				slog.Float64("duration_ms", float64(duration.Microseconds())/1000.0),
				slog.String("remote_addr", r.RemoteAddr),
			)


			rootSpan := SpanData{
				TraceID:    traceID,
				SpanID:     spanID,
				Name:       fmt.Sprintf("%s %s", r.Method, r.URL.Path),
				StartTime:  start,
				EndTime:    time.Now(),
				StatusCode: 1,
				Attributes: map[string]string{
					"http.method":      r.Method,
					"http.route":       r.URL.Path,
					"http.status_code": fmt.Sprintf("%d", sw.statusCode),
				},
			}
			if sw.statusCode >= 500 {
				rootSpan.StatusCode = 2
			}
			select {
			case spanQueue <- rootSpan:
			default:
			}
		}
	})
}

func parseOrCreateTrace(header string) (string, string) {
	parts := strings.Split(header, "-")
	if len(parts) == 4 && len(parts[1]) == 32 && len(parts[2]) == 16 {
		return parts[1], parts[2]
	}
	return generateRandomHex(16), generateRandomHex(8)
}
