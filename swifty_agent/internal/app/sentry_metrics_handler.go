package app

import (
	"bytes"
	"net/http"
	"strconv"

	"github.com/hangtiancheng/swifty.go/swifty_http"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/common/expfmt"
)

// swifty-sentry → Prometheus bridge, mirroring the Next.js lib/metrics.ts:
// the browser SDK posts IReportData batches to POST /api/log, events are
// converted into the metrics below, and GET /api/metrics serves the text
// exposition for Prometheus to scrape.
var (
	sentryRegistry = prometheus.NewRegistry()

	sentryEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_sentry_events_total",
			Help: "Events reported by the swifty-sentry browser SDK, by type and status.",
		},
		[]string{"type", "status", "project_id"},
	)

	sentryHTTPRequestDurationMs = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "swifty_sentry_http_request_duration_ms",
			Help:    "Browser-side HTTP request duration in milliseconds (XHR/fetch events).",
			Buckets: []float64{50, 100, 300, 500, 1000, 3000, 10000},
		},
		[]string{"method", "status_code"},
	)

	sentryWebVitals = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "swifty_sentry_web_vitals",
			Help: "Latest browser performance metric value (LCP/FCP/CLS/INP/TTFB/FSP...).",
		},
		[]string{"name", "rating", "project_id"},
	)
)

func init() {
	sentryRegistry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		sentryEventsTotal,
		sentryHTTPRequestDurationMs,
		sentryWebVitals,
	)
}

// sentryReportItem is the loose wire shape of one SDK IReportData entry;
// unknown fields are ignored by encoding/json.
type sentryReportItem struct {
	Type      string              `json:"type"`
	Name      string              `json:"name"`
	Status    string              `json:"status"`
	ProjectID string              `json:"projectId"`
	Payload   sentryReportPayload `json:"payload"`
}

// sentryReportPayload merges the fields this bridge cares about across the
// SDK payload variants (IHttpData and IPerformanceMetricData).
type sentryReportPayload struct {
	Method      string   `json:"method"`
	StatusCode  *float64 `json:"statusCode"`
	ElapsedTime *float64 `json:"elapsedTime"`
	Value       *float64 `json:"value"`
	Rating      string   `json:"rating"`
}

func recordSentryReportBatch(items []sentryReportItem) {
	for _, item := range items {
		projectID := item.ProjectID
		if projectID == "" {
			projectID = "unknown"
		}
		status := item.Status
		if status == "" {
			status = "unknown"
		}
		sentryEventsTotal.WithLabelValues(item.Type, status, projectID).Inc()

		if (item.Type == "XMLHttpRequest" || item.Type == "fetch") && item.Payload.ElapsedTime != nil {
			method := item.Payload.Method
			if method == "" {
				method = "unknown"
			}
			statusCode := 0
			if item.Payload.StatusCode != nil {
				statusCode = int(*item.Payload.StatusCode)
			}
			sentryHTTPRequestDurationMs.
				WithLabelValues(method, strconv.Itoa(statusCode)).
				Observe(*item.Payload.ElapsedTime)
		}

		if item.Type == "Performance" && item.Name != "" && item.Payload.Value != nil {
			rating := item.Payload.Rating
			if rating == "" {
				rating = "none"
			}
			sentryWebVitals.
				WithLabelValues(item.Name, rating, projectID).
				Set(*item.Payload.Value)
		}
	}
}

// handleSentryLog receives swifty-sentry report batches (the SDK dsn).
// sendBeacon posts text/plain and fetch posts application/json; BindJSON
// decodes either since it never sniffs the content type.
func (a *App) handleSentryLog(ctx *swifty_http.Context, next func()) {
	var items []sentryReportItem
	if err := ctx.BindJSON(&items); err != nil {
		ctx.Throw(http.StatusBadRequest, "invalid report batch")
		return
	}
	recordSentryReportBatch(items)
	ctx.JSON(map[string]any{
		"message": "OK",
		"data":    map[string]any{"received": len(items)},
	})
}

// handleMetrics serves the Prometheus text exposition of the bridge registry.
func (a *App) handleMetrics(ctx *swifty_http.Context, next func()) {
	families, err := sentryRegistry.Gather()
	if err != nil {
		ctx.Throw(http.StatusInternalServerError, err.Error())
		return
	}
	format := expfmt.NewFormat(expfmt.TypeTextPlain)
	var buf bytes.Buffer
	encoder := expfmt.NewEncoder(&buf, format)
	for _, family := range families {
		if err := encoder.Encode(family); err != nil {
			ctx.Throw(http.StatusInternalServerError, err.Error())
			return
		}
	}
	ctx.Set("Content-Type", string(format))
	ctx.Data(buf.Bytes())
}
