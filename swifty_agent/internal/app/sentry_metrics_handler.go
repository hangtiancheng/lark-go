// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package app

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"runtime/metrics"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hangtiancheng/swifty.go/swifty_http"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/common/expfmt"
)

// swifty-sentry → Prometheus bridge, mirroring the Next.js lib/metrics.ts:
// the browser SDK posts IReportData batches to POST /api/log, events are
// converted into the metrics below, and GET /api/metrics serves the text
// exposition for Prometheus to scrape.
//
// Metric names and label sets are deliberately identical to the Next.js bridge
// because one Prometheus instance scrapes both services, so a single set of
// alert rules in prometheus.rules.yml covers both jobs.
//
// Every SDK report type is covered except ScreenRecord, which carries only an
// opaque rrweb blob. History and "Event hashchange" never reach the reporter as
// their own events (they become breadcrumbs plus a PV event), and
// "Event unhandledrejection" is re-dispatched as an Error event.
var (
	sentryRegistry = prometheus.NewRegistry()

	sentryEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_sentry_events_total",
			Help: "Events reported by the swifty-sentry browser SDK, by type and status.",
		},
		[]string{"type", "status", "project_id"},
	)

	sentryEventLastSeen = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "swifty_sentry_event_last_seen_timestamp_seconds",
			Help: "Unix timestamp of the most recent SDK event of each type.",
		},
		[]string{"type", "project_id"},
	)

	sentryErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_sentry_errors_total",
			Help: "Browser errors (Error/React/Vue/OtherFrameworks), counting each event inside a batched group.",
		},
		[]string{"type", "name", "project_id"},
	)

	sentryBatchErrorGroupsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_sentry_batch_error_groups_total",
			Help: "Error bursts the SDK collapsed into a single batched report (5+ identical errors within 2s).",
		},
		[]string{"type", "name", "project_id"},
	)

	sentryResourceErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_sentry_resource_errors_total",
			Help: "Static resource load failures, by failing element tag (img/script/link).",
		},
		[]string{"tag", "project_id"},
	)

	sentryHTTPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_sentry_http_requests_total",
			Help: "Browser-side XHR/fetch requests. Successes only appear when the SDK runs with enableHttpPerformance.",
		},
		[]string{"method", "status_code", "status", "project_id"},
	)

	sentryHTTPRequestDurationMs = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "swifty_sentry_http_request_duration_ms",
			Help:    "Browser-side HTTP request duration in milliseconds (XHR/fetch events).",
			Buckets: []float64{50, 100, 300, 500, 1000, 3000, 10000},
		},
		[]string{"method", "status_code", "project_id"},
	)

	sentryWebVitals = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "swifty_sentry_web_vitals",
			Help: "Latest web vital value (LCP/FCP/INP/TTFB/FSP in ms, CLS unitless).",
		},
		[]string{"name", "rating", "project_id"},
	)

	sentryWebVitalSamplesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_sentry_web_vital_samples_total",
			Help: "Web vital samples by rating, for rate-based degradation alerts.",
		},
		[]string{"name", "rating", "project_id"},
	)

	sentryNavigationTimingMs = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "swifty_sentry_navigation_timing_ms",
			Help:    "Page navigation timing phases in milliseconds.",
			Buckets: []float64{10, 50, 100, 300, 500, 1000, 2000, 5000, 10000, 30000},
		},
		[]string{"phase", "project_id"},
	)

	sentryResourceEntriesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_sentry_resource_entries_total",
			Help: "Static resource timing entries, by initiator type and cache hit.",
		},
		[]string{"initiator_type", "from_cache", "project_id"},
	)

	sentryResourceDurationMs = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "swifty_sentry_resource_duration_ms",
			Help:    "Static resource load duration in milliseconds.",
			Buckets: []float64{10, 50, 100, 300, 500, 1000, 3000, 10000},
		},
		[]string{"initiator_type", "from_cache", "project_id"},
	)

	sentryResourceTransferBytes = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "swifty_sentry_resource_transfer_bytes",
			Help:    "Static resource transfer size in bytes (0 for cache hits).",
			Buckets: []float64{1024, 10240, 51200, 102400, 512000, 1048576, 5242880},
		},
		[]string{"initiator_type", "project_id"},
	)

	sentryLongTasksTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_sentry_long_tasks_total",
			Help: "Main-thread long tasks observed in the browser.",
		},
		[]string{"project_id"},
	)

	sentryLongTaskDurationMs = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "swifty_sentry_long_task_duration_ms",
			Help:    "Main-thread long task duration in milliseconds.",
			Buckets: []float64{50, 100, 200, 500, 1000, 2000, 5000},
		},
		[]string{"project_id"},
	)

	sentryBrowserMemoryBytes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "swifty_sentry_browser_memory_bytes",
			Help: "Browser tab memory from performance.measureUserAgentSpecificMemory (Chromium only).",
		},
		[]string{"project_id"},
	)

	sentryBrowserMemoryBreakdownBytes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "swifty_sentry_browser_memory_breakdown_bytes",
			Help: "Browser tab memory breakdown by reported allocation type.",
		},
		[]string{"kind", "project_id"},
	)

	sentryClicksTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_sentry_clicks_total",
			Help: "Declarative click events, by swifty-sentry-ev identifier.",
		},
		[]string{"ev", "project_id"},
	)

	sentryExposuresTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_sentry_exposures_total",
			Help: "Element exposure events completed (element left the viewport after being visible).",
		},
		[]string{"project_id"},
	)

	sentryExposureDurationMs = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "swifty_sentry_exposure_duration_ms",
			Help:    "Element visible duration in milliseconds.",
			Buckets: []float64{100, 500, 1000, 3000, 10000, 30000, 60000},
		},
		[]string{"project_id"},
	)

	sentryWhiteScreensTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_sentry_white_screens_total",
			Help: "White-screen detections reported by the SDK sampler.",
		},
		[]string{"project_id"},
	)

	sentryPageViewsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_sentry_page_views_total",
			Help: "Page view events, by lifecycle name (PageLoad/HistoryChange/HashChange/ManualPageView/PageDwell).",
		},
		[]string{"name", "project_id"},
	)

	sentryPageDwellMs = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "swifty_sentry_page_dwell_ms",
			Help:    "Time spent on a page before navigating away, in milliseconds.",
			Buckets: []float64{1000, 5000, 15000, 30000, 60000, 300000, 900000},
		},
		[]string{"project_id"},
	)

	sentryCustomEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_sentry_custom_events_total",
			Help: "Business events reported through traceCustomEvent.",
		},
		[]string{"name", "project_id"},
	)

	sentryPerformanceValue = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "swifty_sentry_performance_value",
			Help: "Latest value of Performance events that are not web vitals (e.g. tracePerformance metrics).",
		},
		[]string{"name", "project_id"},
	)

	sentryReportBatchesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "swifty_sentry_report_batches_total",
			Help: "Report batches received at /api/log, by validation outcome.",
		},
		[]string{"outcome"},
	)

	sentryReportBatchSize = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "swifty_sentry_report_batch_size",
			Help:    "Number of events per accepted report batch.",
			Buckets: []float64{1, 2, 5, 10, 20, 50, 100},
		},
	)
)

// noLimit is the value runtime/metrics reports for /gc/gomemlimit:bytes when
// GOMEMLIMIT is unset, i.e. math.MaxInt64.
const noLimit = uint64(math.MaxInt64)

func init() {
	sentryRegistry.MustRegister(
		// The default GoCollector exposes only go_memstats_*, go_goroutines,
		// go_threads and the go_gc_duration_seconds summary. Opting into
		// runtime/metrics adds what actually matters for tracing a Go service:
		// /sched/latencies (the analogue of event loop lag), /sched/pauses,
		// per-goroutine-state counts, /gc/pauses, /gc/heap/live, /memory/classes/*,
		// /cpu/classes/* (GC CPU share) and /sync/mutex/wait (contention).
		// /godebug/* is excluded: 50-odd always-zero series with no triage value.
		collectors.NewGoCollector(
			collectors.WithGoCollectorRuntimeMetrics(
				collectors.MetricsGC,
				collectors.MetricsMemory,
				collectors.MetricsScheduler,
				collectors.GoRuntimeMetricsRule{Matcher: regexp.MustCompile(`^/cpu/classes/.*`)},
				collectors.GoRuntimeMetricsRule{Matcher: regexp.MustCompile(`^/sync/.*`)},
				collectors.GoRuntimeMetricsRule{Matcher: regexp.MustCompile(`^/cgo/.*`)},
			),
		),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewBuildInfoCollector(),

		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "swifty_go_memory_limit_bytes",
				Help: "GOMEMLIMIT soft memory limit in bytes; 0 when no limit is configured.",
			},
			func() float64 {
				limit, ok := readRuntimeUint64("/gc/gomemlimit:bytes")
				if !ok || limit == noLimit {
					return 0
				}
				return float64(limit)
			},
		),

		// The Go analogue of swifty_node_v8_heap_used_ratio. Without GOMEMLIMIT
		// the runtime has no ceiling of its own, so the ratio is reported as 0
		// rather than inventing one; use process_resident_memory_bytes instead.
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: "swifty_go_heap_used_ratio",
				Help: "Live heap divided by GOMEMLIMIT; 0 when GOMEMLIMIT is unset. 1.0 means an imminent OOM.",
			},
			func() float64 {
				limit, okLimit := readRuntimeUint64("/gc/gomemlimit:bytes")
				live, okLive := readRuntimeUint64("/gc/heap/live:bytes")
				if !okLimit || !okLive || limit == noLimit || limit == 0 {
					return 0
				}
				return float64(live) / float64(limit)
			},
		),

		sentryEventsTotal,
		sentryEventLastSeen,
		sentryErrorsTotal,
		sentryBatchErrorGroupsTotal,
		sentryResourceErrorsTotal,
		sentryHTTPRequestsTotal,
		sentryHTTPRequestDurationMs,
		sentryWebVitals,
		sentryWebVitalSamplesTotal,
		sentryNavigationTimingMs,
		sentryResourceEntriesTotal,
		sentryResourceDurationMs,
		sentryResourceTransferBytes,
		sentryLongTasksTotal,
		sentryLongTaskDurationMs,
		sentryBrowserMemoryBytes,
		sentryBrowserMemoryBreakdownBytes,
		sentryClicksTotal,
		sentryExposuresTotal,
		sentryExposureDurationMs,
		sentryWhiteScreensTotal,
		sentryPageViewsTotal,
		sentryPageDwellMs,
		sentryCustomEventsTotal,
		sentryPerformanceValue,
		sentryReportBatchesTotal,
		sentryReportBatchSize,
	)
}

// readRuntimeUint64 reads a single uint64-valued runtime/metrics sample.
// Unknown metric names yield KindBad and report not-ok, so a Go version that
// drops a metric degrades to 0 instead of panicking.
func readRuntimeUint64(name string) (uint64, bool) {
	sample := []metrics.Sample{{Name: name}}
	metrics.Read(sample)
	if sample[0].Value.Kind() != metrics.KindUint64 {
		return 0, false
	}
	return sample[0].Value.Uint64(), true
}

// webVitalNames are the only Performance events measured on the shared
// swifty_sentry_web_vitals gauge. Every other Performance event carries an
// unrelated unit (bytes, task counts) and must not land in the same series.
var webVitalNames = map[string]struct{}{
	"LCP": {}, "FCP": {}, "CLS": {}, "INP": {}, "TTFB": {}, "FSP": {},
}

// navigationPhases bounds the phase label to the fields the SDK actually emits
// in a NavigationTiming payload.
var navigationPhases = []string{
	"paintTime",
	"domInteractive",
	"domContentLoaded",
	"loadEvent",
	"firstByte",
	"dnsLookup",
	"tcpConnection",
	"tlsHandshake",
	"timeToFirstByte",
	"contentTransfer",
	"domProcessing",
	"resourceLoad",
	"redirect",
	"unloadTime",
}

// maxLabelValues caps distinct values per browser-supplied label. Error names,
// click ids and custom event names are attacker- and refactor-controlled, so
// collapsing the tail keeps a bad deploy from exploding Prometheus series count.
const maxLabelValues = 50

var labelValues = struct {
	sync.Mutex
	seen map[string]map[string]struct{}
}{seen: make(map[string]map[string]struct{})}

func boundedLabel(key, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "unknown"
	}
	labelValues.Lock()
	defer labelValues.Unlock()
	seen, ok := labelValues.seen[key]
	if !ok {
		seen = make(map[string]struct{})
		labelValues.seen[key] = seen
	}
	if _, ok := seen[trimmed]; ok {
		return trimmed
	}
	if len(seen) >= maxLabelValues {
		return "other"
	}
	seen[trimmed] = struct{}{}
	return trimmed
}

// sentryReportItem is the loose wire shape of one SDK IReportData entry.
// Payload stays raw so each event type can decode only the variant it needs.
type sentryReportItem struct {
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	Status    string          `json:"status"`
	ProjectID string          `json:"projectId"`
	Payload   json.RawMessage `json:"payload"`
}

type sentryHTTPPayload struct {
	Method      string   `json:"method"`
	StatusCode  *float64 `json:"statusCode"`
	ElapsedTime *float64 `json:"elapsedTime"`
}

type sentryBatchErrorPayload struct {
	BatchError       bool     `json:"batchError"`
	BatchErrorLength *float64 `json:"batchErrorLength"`
}

type sentryResourceTiming struct {
	InitiatorType string   `json:"initiatorType"`
	Duration      *float64 `json:"duration"`
	TransferSize  *float64 `json:"transferSize"`
	FromCache     bool     `json:"fromCache"`
}

type sentryMemoryBreakdown struct {
	Bytes float64  `json:"bytes"`
	Types []string `json:"types"`
}

type sentryPerformancePayload struct {
	Value        *float64               `json:"value"`
	Rating       string                 `json:"rating"`
	Extra        json.RawMessage        `json:"extra"`
	ResourceList []sentryResourceTiming `json:"resourceList"`
	LongTasks    []struct {
		Duration *float64 `json:"duration"`
	} `json:"longTasks"`
	Memory *struct {
		Bytes     *float64                `json:"bytes"`
		Breakdown []sentryMemoryBreakdown `json:"breakdown"`
	} `json:"memory"`
}

type sentryExtraCarrier struct {
	Extra json.RawMessage `json:"extra"`
}

type sentryClickExtra struct {
	Ev  string `json:"ev"`
	Msg string `json:"msg"`
}

type sentryDurationExtra struct {
	Duration *float64 `json:"duration"`
}

type sentryResourceExtra struct {
	Resource sentryResourceTiming `json:"resource"`
}

// decodePayload unmarshals a raw payload into target, reporting whether the
// payload was present and well-formed. Report payloads are heterogeneous, so a
// mismatch is expected rather than exceptional and is simply skipped.
func decodePayload(raw json.RawMessage, target any) bool {
	if len(raw) == 0 {
		return false
	}
	return json.Unmarshal(raw, target) == nil
}

// readExtra pulls the raw `extra` field out of a payload.
func readExtra(raw json.RawMessage) json.RawMessage {
	var carrier sentryExtraCarrier
	if !decodePayload(raw, &carrier) {
		return nil
	}
	return carrier.Extra
}

func recordSentryReportBatch(items []sentryReportItem) {
	sentryReportBatchesTotal.WithLabelValues("accepted").Inc()
	sentryReportBatchSize.Observe(float64(len(items)))
	for _, item := range items {
		recordSentryReportItem(item)
	}
}

func recordSentryReportItem(item sentryReportItem) {
	projectID := item.ProjectID
	if projectID == "" {
		projectID = "unknown"
	}
	status := item.Status
	if status == "" {
		status = "unknown"
	}

	sentryEventsTotal.WithLabelValues(item.Type, status, projectID).Inc()
	sentryEventLastSeen.WithLabelValues(item.Type, projectID).
		Set(float64(time.Now().UnixNano()) / float64(time.Second))

	switch item.Type {
	case "XMLHttpRequest", "fetch":
		recordSentryHTTPEvent(item, projectID, status)
	case "Error", "React", "Vue", "OtherFrameworks":
		recordSentryErrorEvent(item, projectID)
	case "Resource":
		sentryResourceErrorsTotal.
			WithLabelValues(boundedLabel("resource_tag", item.Name), projectID).Inc()
	case "Performance":
		recordSentryPerformanceEvent(item, projectID)
	case "Click":
		recordSentryClickEvent(item, projectID)
	case "Exposure":
		recordSentryExposureEvent(item, projectID)
	case "WhiteScreen":
		sentryWhiteScreensTotal.WithLabelValues(projectID).Inc()
	case "PV":
		recordSentryPageViewEvent(item, projectID)
	case "Custom":
		sentryCustomEventsTotal.
			WithLabelValues(boundedLabel("custom_event", item.Name), projectID).Inc()
	}
}

func recordSentryHTTPEvent(item sentryReportItem, projectID, status string) {
	var payload sentryHTTPPayload
	decodePayload(item.Payload, &payload)

	method := payload.Method
	if method == "" {
		method = "unknown"
	}
	statusCode := "0"
	if payload.StatusCode != nil {
		statusCode = strconv.Itoa(int(*payload.StatusCode))
	}

	sentryHTTPRequestsTotal.WithLabelValues(method, statusCode, status, projectID).Inc()
	if payload.ElapsedTime != nil {
		sentryHTTPRequestDurationMs.
			WithLabelValues(method, statusCode, projectID).
			Observe(*payload.ElapsedTime)
	}
}

func recordSentryErrorEvent(item sentryReportItem, projectID string) {
	name := boundedLabel("error_name", item.Name)

	var batch sentryBatchErrorPayload
	if decodePayload(item.Payload, &batch) && batch.BatchError && batch.BatchErrorLength != nil {
		sentryBatchErrorGroupsTotal.WithLabelValues(item.Type, name, projectID).Inc()
		sentryErrorsTotal.WithLabelValues(item.Type, name, projectID).Add(*batch.BatchErrorLength)
		return
	}
	sentryErrorsTotal.WithLabelValues(item.Type, name, projectID).Inc()
}

func recordSentryPerformanceEvent(item sentryReportItem, projectID string) {
	var payload sentryPerformancePayload
	decodePayload(item.Payload, &payload)

	if _, ok := webVitalNames[item.Name]; ok {
		recordSentryWebVital(item.Name, payload, projectID)
		return
	}

	switch item.Name {
	case "NavigationTiming":
		recordSentryNavigationTiming(payload.Extra, projectID)
		return
	case "ResourceTiming":
		var extra sentryResourceExtra
		if decodePayload(payload.Extra, &extra) {
			recordSentryResourceEntry(extra.Resource, projectID)
		}
		return
	case "ResourceList":
		for _, resource := range payload.ResourceList {
			recordSentryResourceEntry(resource, projectID)
		}
		return
	case "LongTask":
		for _, task := range payload.LongTasks {
			sentryLongTasksTotal.WithLabelValues(projectID).Inc()
			if task.Duration != nil {
				sentryLongTaskDurationMs.WithLabelValues(projectID).Observe(*task.Duration)
			}
		}
		return
	case "Memory":
		recordSentryBrowserMemory(payload, projectID)
		return
	}

	// enableHttpPerformance reports successful requests as "HTTP <METHOD>".
	if strings.HasPrefix(item.Name, "HTTP ") {
		recordSentryHTTPPerformance(item.Name, payload, projectID)
		return
	}

	if payload.Value != nil {
		sentryPerformanceValue.
			WithLabelValues(boundedLabel("performance_name", item.Name), projectID).
			Set(*payload.Value)
	}
}

func recordSentryWebVital(name string, payload sentryPerformancePayload, projectID string) {
	if payload.Value == nil {
		return
	}
	rating := payload.Rating
	if rating == "" {
		rating = "none"
	}
	sentryWebVitals.WithLabelValues(name, rating, projectID).Set(*payload.Value)
	sentryWebVitalSamplesTotal.WithLabelValues(name, rating, projectID).Inc()
}

func recordSentryNavigationTiming(extra json.RawMessage, projectID string) {
	// triggerPageUrl makes the object heterogeneous, so decode loosely and keep
	// only the numeric phases.
	var values map[string]any
	if !decodePayload(extra, &values) {
		return
	}
	for _, phase := range navigationPhases {
		if value, ok := values[phase].(float64); ok {
			sentryNavigationTimingMs.WithLabelValues(phase, projectID).Observe(value)
		}
	}
}

func recordSentryResourceEntry(resource sentryResourceTiming, projectID string) {
	initiatorType := boundedLabel("initiator_type", resource.InitiatorType)
	fromCache := strconv.FormatBool(resource.FromCache)

	sentryResourceEntriesTotal.WithLabelValues(initiatorType, fromCache, projectID).Inc()
	if resource.Duration != nil {
		sentryResourceDurationMs.
			WithLabelValues(initiatorType, fromCache, projectID).
			Observe(*resource.Duration)
	}
	if resource.TransferSize != nil {
		sentryResourceTransferBytes.
			WithLabelValues(initiatorType, projectID).
			Observe(*resource.TransferSize)
	}
}

func recordSentryBrowserMemory(payload sentryPerformancePayload, projectID string) {
	if payload.Memory == nil {
		return
	}
	if payload.Memory.Bytes != nil {
		sentryBrowserMemoryBytes.WithLabelValues(projectID).Set(*payload.Memory.Bytes)
	}
	totals := make(map[string]float64)
	for _, entry := range payload.Memory.Breakdown {
		totals[boundedLabel("browser_memory_kind", strings.Join(entry.Types, "+"))] += entry.Bytes
	}
	for kind, value := range totals {
		sentryBrowserMemoryBreakdownBytes.WithLabelValues(kind, projectID).Set(value)
	}
}

func recordSentryHTTPPerformance(name string, payload sentryPerformancePayload, projectID string) {
	method := strings.TrimPrefix(name, "HTTP ")
	statusCode := "0"

	var extra sentryHTTPPayload
	if decodePayload(payload.Extra, &extra) {
		if extra.Method != "" {
			method = extra.Method
		}
		if extra.StatusCode != nil {
			statusCode = strconv.Itoa(int(*extra.StatusCode))
		}
	}

	sentryHTTPRequestsTotal.WithLabelValues(method, statusCode, "OK", projectID).Inc()
	if payload.Value != nil {
		sentryHTTPRequestDurationMs.
			WithLabelValues(method, statusCode, projectID).
			Observe(*payload.Value)
	}
}

func recordSentryClickEvent(item sentryReportItem, projectID string) {
	ev := item.Name
	var extra sentryClickExtra
	if decodePayload(readExtra(item.Payload), &extra) && extra.Ev != "" {
		ev = extra.Ev
	}
	sentryClicksTotal.WithLabelValues(boundedLabel("click_ev", ev), projectID).Inc()
}

func recordSentryExposureEvent(item sentryReportItem, projectID string) {
	sentryExposuresTotal.WithLabelValues(projectID).Inc()

	var extra sentryDurationExtra
	if decodePayload(readExtra(item.Payload), &extra) && extra.Duration != nil {
		sentryExposureDurationMs.WithLabelValues(projectID).Observe(*extra.Duration)
	}
}

func recordSentryPageViewEvent(item sentryReportItem, projectID string) {
	sentryPageViewsTotal.
		WithLabelValues(boundedLabel("page_view_name", item.Name), projectID).Inc()
	if item.Name != "PageDwell" {
		return
	}
	var extra sentryDurationExtra
	if decodePayload(readExtra(item.Payload), &extra) && extra.Duration != nil {
		sentryPageDwellMs.WithLabelValues(projectID).Observe(*extra.Duration)
	}
}

// handleSentryLog receives swifty-sentry report batches (the SDK dsn).
// sendBeacon posts text/plain and fetch posts application/json; BindJSON
// decodes either since it never sniffs the content type.
func (a *App) handleSentryLog(ctx *swifty_http.Context, next func()) {
	var items []sentryReportItem
	if err := ctx.BindJSON(&items); err != nil {
		sentryReportBatchesTotal.WithLabelValues("invalid").Inc()
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
