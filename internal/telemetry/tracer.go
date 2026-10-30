package telemetry

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

var (
	otlpEndpoint = "http://vtsingle-vmstack-victoria-metrics-k8s-stack.monitoring.svc.cluster.local:10428/insert/opentelemetry/v1/traces"
	spanQueue    = make(chan SpanData, 1000)
	client       = &http.Client{Timeout: 3 * time.Second}
	workerOnce   sync.Once
)

type SpanData struct {
	TraceID      string
	SpanID       string
	ParentSpanID string
	Name         string
	StartTime    time.Time
	EndTime      time.Time
	StatusCode   int // 1 = OK, 2 = ERROR
	Attributes   map[string]string
}

func init() {
	workerOnce.Do(func() {
		go processSpanQueue()
	})
}

func StartSpan(ctx context.Context, spanName string) (context.Context, *Span) {
	traceID, _ := ctx.Value(TraceIDKey).(string)
	parentSpanID, _ := ctx.Value(SpanIDKey).(string)

	childSpanID := generateRandomHex(8)
	newCtx := context.WithValue(ctx, SpanIDKey, childSpanID)

	span := &Span{
		data: SpanData{
			TraceID:      traceID,
			SpanID:       childSpanID,
			ParentSpanID: parentSpanID,
			Name:         spanName,
			StartTime:    time.Now(),
			StatusCode:   1,
			Attributes:   make(map[string]string),
		},
	}
	return newCtx, span
}

type Span struct {
	data SpanData
}

func (s *Span) SetAttribute(key, value string) {
	s.data.Attributes[key] = value
}

func (s *Span) SetError() {
	s.data.StatusCode = 2
}

func (s *Span) End() {
	s.data.EndTime = time.Now()
	select {
	case spanQueue <- s.data:
	default:
	}
}

func generateRandomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func processSpanQueue() {
	for span := range spanQueue {
		payload := buildOTLPPayload(span)
		body, err := json.Marshal(payload)
		if err != nil {
			Logger.Error("tracer_json_marshal_error", slog.String("error", err.Error()))
			continue
		}

		req, err := http.NewRequest("POST", otlpEndpoint, bytes.NewReader(body))
		if err != nil {
			Logger.Error("tracer_request_creation_error", slog.String("error", err.Error()))
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			Logger.Error("tracer_export_connection_failed", slog.String("error", err.Error()))
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			respBody, _ := io.ReadAll(resp.Body)
			Logger.Error("tracer_victoria_traces_rejected",
				slog.Int("status", resp.StatusCode),
				slog.String("response", string(respBody)),
			)
		}
		_ = resp.Body.Close()
	}
}

func buildOTLPPayload(s SpanData) map[string]any {
	attrs := make([]map[string]any, 0, len(s.Attributes))
	for k, v := range s.Attributes {
		attrs = append(attrs, map[string]any{
			"key": k,
			"value": map[string]any{
				"stringValue": v,
			},
		})
	}

	spanKind := 1
	if s.ParentSpanID == "" {
		spanKind = 2
	}

	spanObj := map[string]any{
		"traceId":           s.TraceID,
		"spanId":            s.SpanID,
		"name":              s.Name,
		"kind":              spanKind,
		"startTimeUnixNano": fmt.Sprintf("%d", s.StartTime.UnixNano()),
		"endTimeUnixNano":   fmt.Sprintf("%d", s.EndTime.UnixNano()),
		"attributes":        attrs,
		"status": map[string]any{
			"code": s.StatusCode,
		},
	}

	if s.ParentSpanID != "" {
		spanObj["parentSpanId"] = s.ParentSpanID
	}

	return map[string]any{
		"resourceSpans": []map[string]any{
			{
				"resource": map[string]any{
					"attributes": []map[string]any{
						{
							"key": "service.name",
							"value": map[string]any{
								"stringValue": "go-http-server",
							},
						},
					},
				},
				"scopeSpans": []map[string]any{
					{
						"scope": map[string]any{"name": "custom-http-tracer"},
						"spans": []map[string]any{spanObj},
					},
				},
			},
		},
	}
}
