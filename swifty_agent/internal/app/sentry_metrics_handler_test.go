package app

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	test_util "github.com/prometheus/client_golang/prometheus/testutil"
)

// resourceTimingJSON is one IPerformanceResourceTiming as the SDK serializes it.
const resourceTimingJSON = `{
	"name": "http://127.0.0.1:8123/app.js",
	"initiatorType": "script",
	"startTime": 10,
	"responseEnd": 120,
	"duration": 110,
	"transferSize": 45678,
	"encodedBodySize": 45000,
	"decodedBodySize": 120000,
	"fromCache": false
}`

func item(eventType, name, status, payload string) sentryReportItem {
	return sentryReportItem{
		Type:      eventType,
		Name:      name,
		Status:    status,
		ProjectID: "smoke",
		Payload:   json.RawMessage(payload),
	}
}

// syntheticBatch carries one report per swifty-sentry event type, including
// ScreenRecord, which must only move the generic events counter.
func syntheticBatch() []sentryReportItem {
	return []sentryReportItem{
		item("XMLHttpRequest", "http://127.0.0.1:8123/api/boom", "Error",
			`{"method":"GET","api":"/api/boom","statusCode":500,"elapsedTime":432}`),
		item("fetch", "http://127.0.0.1:8123/api/nope", "Error",
			`{"method":"POST","api":"/api/nope","statusCode":404,"elapsedTime":88}`),
		item("Error", "http://127.0.0.1:8123/chunk.js", "Error", `{"line":12,"column":34}`),
		item("Error", "TypeError", "Error",
			`{"batchError":true,"batchErrorLength":7,"batchErrorLastHappenTime":1}`),
		item("React", "Error", "Error", `{"extra":{"stack":"at CrashingProbe"}}`),
		item("Vue", "Error", "Error", `{"extra":{"stack":"at setup"}}`),
		item("OtherFrameworks", "Error", "Error", `{"extra":{"stack":"at mount"}}`),
		item("Resource", "img", "Error", `{"src":"/missing.png","href":""}`),
		item("Click", "nav-home", "OK", `{"extra":{"ev":"nav-home","msg":"Go home"}}`),
		item("Exposure", "Exposure", "OK", `{"extra":{"threshold":0.5,"duration":4000}}`),
		item("WhiteScreen", "WhiteScreen", "Error", `{"extra":"WhiteScreen"}`),
		item("PV", "PageLoad", "OK", `{"extra":{"url":"/","referrer":""}}`),
		item("PV", "PageDwell", "OK", `{"extra":{"url":"/","duration":27000}}`),
		item("Custom", "CheckoutSuccess", "OK", `{"extra":{"orderId":"order-1"}}`),
		item("Performance", "LCP", "OK", `{"value":2310,"rating":"good"}`),
		item("Performance", "CLS", "OK", `{"value":0.31,"rating":"poor"}`),
		item("Performance", "INP", "OK", `{"value":640,"rating":"poor"}`),
		item("Performance", "TTFB", "OK", `{"value":410,"rating":"good"}`),
		item("Performance", "FCP", "OK", `{"value":1200,"rating":"good"}`),
		item("Performance", "FSP", "OK", `{"value":1500}`),
		item("Performance", "NavigationTiming", "OK", `{"extra":{
			"paintTime":900,"domInteractive":800,"domContentLoaded":1100,"loadEvent":1400,
			"firstByte":300,"dnsLookup":5,"tcpConnection":12,"tlsHandshake":20,
			"timeToFirstByte":250,"contentTransfer":40,"domProcessing":460,
			"resourceLoad":300,"redirect":0,"unloadTime":0,
			"triggerPageUrl":"http://127.0.0.1:8123/"}}`),
		item("Performance", "ResourceTiming", "OK",
			`{"value":110,"extra":{"resource":`+resourceTimingJSON+`}}`),
		item("Performance", "ResourceList", "OK",
			`{"resourceList":[`+resourceTimingJSON+`,
				{"name":"/app.css","initiatorType":"link","duration":3,"transferSize":0,"fromCache":true}]}`),
		item("Performance", "LongTask", "OK",
			`{"longTasks":[{"duration":320},{"duration":75}]}`),
		item("Performance", "Memory", "OK", `{"memory":{"bytes":41000000,"breakdown":[
			{"bytes":30000000,"types":["JavaScript"]},
			{"bytes":11000000,"types":["DOM"]}]}}`),
		item("Performance", "HTTP GET", "OK",
			`{"value":145,"extra":{"method":"GET","statusCode":200,"serverTiming":[]}}`),
		item("Performance", "SearchLatency", "OK", `{"value":128}`),
		item("ScreenRecord", "ScreenRecord", "OK", `{"events":"gzipped","eventCount":12}`),
	}
}

// expectedFamilies lists every metric family the bridge must expose. The
// swifty_sentry_* names are shared with the Next.js bridge so one set of alert
// rules covers both jobs; the go_* names come from the opted-in runtime metrics.
var expectedFamilies = []string{
	"swifty_sentry_events_total",
	"swifty_sentry_event_last_seen_timestamp_seconds",
	"swifty_sentry_errors_total",
	"swifty_sentry_batch_error_groups_total",
	"swifty_sentry_resource_errors_total",
	"swifty_sentry_http_requests_total",
	"swifty_sentry_http_request_duration_ms",
	"swifty_sentry_web_vitals",
	"swifty_sentry_web_vital_samples_total",
	"swifty_sentry_navigation_timing_ms",
	"swifty_sentry_resource_entries_total",
	"swifty_sentry_resource_duration_ms",
	"swifty_sentry_resource_transfer_bytes",
	"swifty_sentry_long_tasks_total",
	"swifty_sentry_long_task_duration_ms",
	"swifty_sentry_browser_memory_bytes",
	"swifty_sentry_browser_memory_breakdown_bytes",
	"swifty_sentry_clicks_total",
	"swifty_sentry_exposures_total",
	"swifty_sentry_exposure_duration_ms",
	"swifty_sentry_white_screens_total",
	"swifty_sentry_page_views_total",
	"swifty_sentry_page_dwell_ms",
	"swifty_sentry_custom_events_total",
	"swifty_sentry_performance_value",
	"swifty_sentry_report_batches_total",
	"swifty_sentry_report_batch_size",
	"swifty_go_memory_limit_bytes",
	"swifty_go_heap_used_ratio",
	// Runtime coverage that the default GoCollector does not provide.
	"go_sched_latencies_seconds",
	"go_sched_goroutines_goroutines",
	"go_gc_pauses_seconds",
	"go_gc_heap_live_bytes",
	"go_memory_classes_heap_objects_bytes",
	"go_memory_classes_total_bytes",
	"go_cpu_classes_gc_total_cpu_seconds_total",
	"go_cpu_classes_total_cpu_seconds_total",
	"go_sync_mutex_wait_total_seconds_total",
	"go_goroutines",
	"go_threads",
	"process_cpu_seconds_total",
	"process_resident_memory_bytes",
}

// The metric registry is package-level, so the synthetic batch is ingested
// exactly once here; individual tests only read the resulting state.
func TestMain(m *testing.M) {
	recordSentryReportBatch(syntheticBatch())
	sentryReportBatchesTotal.WithLabelValues("invalid").Inc()
	os.Exit(m.Run())
}

func TestSentryBridgeExposesEveryMetricFamily(t *testing.T) {
	families, err := sentryRegistry.Gather()
	if err != nil {
		t.Fatalf("gather registry: %v", err)
	}

	present := make(map[string]bool, len(families))
	for _, family := range families {
		present[family.GetName()] = true
	}

	for _, name := range expectedFamilies {
		if !present[name] {
			t.Errorf("missing metric family %q", name)
		}
	}
}

func TestSentryBridgeRecordsEventDetail(t *testing.T) {
	cases := []struct {
		name   string
		got    float64
		want   float64
		reason string
	}{
		{
			name:   "batched errors count every event in the group",
			got:    test_util.ToFloat64(sentryErrorsTotal.WithLabelValues("Error", "TypeError", "smoke")),
			want:   7,
			reason: "batchErrorLength is 7, so the counter must advance by 7 rather than 1",
		},
		{
			name:   "batched groups are counted once",
			got:    test_util.ToFloat64(sentryBatchErrorGroupsTotal.WithLabelValues("Error", "TypeError", "smoke")),
			want:   1,
			reason: "one collapsed report is one group",
		},
		{
			name:   "react crashes are attributed to their own type",
			got:    test_util.ToFloat64(sentryErrorsTotal.WithLabelValues("React", "Error", "smoke")),
			want:   1,
			reason: "React events must not be folded into the generic Error type",
		},
		{
			name:   "ResourceTiming and ResourceList share one counter",
			got:    test_util.ToFloat64(sentryResourceEntriesTotal.WithLabelValues("script", "false", "smoke")),
			want:   2,
			reason: "one single entry plus one entry inside the initial resource list",
		},
		{
			name:   "cached resources are labelled separately",
			got:    test_util.ToFloat64(sentryResourceEntriesTotal.WithLabelValues("link", "true", "smoke")),
			want:   1,
			reason: "from_cache must distinguish cache hits",
		},
		{
			name:   "long tasks are counted per entry, not per report",
			got:    test_util.ToFloat64(sentryLongTasksTotal.WithLabelValues("smoke")),
			want:   2,
			reason: "the payload carries two longtask entries",
		},
		{
			name:   "FSP has no web-vitals rating",
			got:    test_util.ToFloat64(sentryWebVitals.WithLabelValues("FSP", "none", "smoke")),
			want:   1500,
			reason: "FSP is a custom SDK metric, so rating degrades to none",
		},
		{
			name:   "non-vital Performance events stay off the web vitals gauge",
			got:    test_util.ToFloat64(sentryPerformanceValue.WithLabelValues("SearchLatency", "smoke")),
			want:   128,
			reason: "tracePerformance metrics have their own gauge",
		},
		{
			name:   "enableHttpPerformance events become OK requests",
			got:    test_util.ToFloat64(sentryHTTPRequestsTotal.WithLabelValues("GET", "200", "OK", "smoke")),
			want:   1,
			reason: "HTTP <METHOD> Performance events carry successful requests",
		},
		{
			name:   "failed requests keep their status code",
			got:    test_util.ToFloat64(sentryHTTPRequestsTotal.WithLabelValues("GET", "500", "Error", "smoke")),
			want:   1,
			reason: "status_code must survive as a label",
		},
		{
			name:   "declarative clicks are keyed by ev",
			got:    test_util.ToFloat64(sentryClicksTotal.WithLabelValues("nav-home", "smoke")),
			want:   1,
			reason: "the ev attribute identifies the click target",
		},
		{
			name:   "white screens are counted",
			got:    test_util.ToFloat64(sentryWhiteScreensTotal.WithLabelValues("smoke")),
			want:   1,
			reason: "white screen is a critical signal and must not be dropped",
		},
		{
			name:   "browser memory breakdown is grouped by type",
			got:    test_util.ToFloat64(sentryBrowserMemoryBreakdownBytes.WithLabelValues("JavaScript", "smoke")),
			want:   30000000,
			reason: "breakdown entries are summed per reported allocation type",
		},
		{
			name:   "page views are split by lifecycle name",
			got:    test_util.ToFloat64(sentryPageViewsTotal.WithLabelValues("PageDwell", "smoke")),
			want:   1,
			reason: "PageDwell must be distinguishable from PageLoad",
		},
		{
			name:   "ScreenRecord only moves the generic counter",
			got:    test_util.ToFloat64(sentryEventsTotal.WithLabelValues("ScreenRecord", "OK", "smoke")),
			want:   1,
			reason: "the rrweb blob has no detail metric by design",
		},
	}

	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: got %v, want %v (%s)", tc.name, tc.got, tc.want, tc.reason)
		}
	}
}

func TestBoundedLabelCollapsesUnboundedValues(t *testing.T) {
	for i := range maxLabelValues {
		if got := boundedLabel("test_key", "value-"+strconv.Itoa(i)); got == "other" {
			t.Fatalf("value %d was collapsed before the cap was reached", i)
		}
	}
	if got := boundedLabel("test_key", "one-too-many"); got != "other" {
		t.Errorf("value past the cap: got %q, want \"other\"", got)
	}
	// Already-seen values keep their identity once the cap is reached.
	if got := boundedLabel("test_key", "value-0"); got != "value-0" {
		t.Errorf("known value after the cap: got %q, want \"value-0\"", got)
	}
	if got := boundedLabel("test_key", "   "); got != "unknown" {
		t.Errorf("blank value: got %q, want \"unknown\"", got)
	}
}

// Every numeric field in a report payload is decoded into a pointer, because
// the SDK omits fields that do not apply to an event type. A missing or
// wrongly-typed field must be skipped, never dereferenced.
func TestSentryBridgeSurvivesMalformedPayloads(t *testing.T) {
	malformed := []sentryReportItem{
		{Type: "Error", Name: "NoPayload", Status: "Error"},
		item("Error", "NullPayload", "Error", `null`),
		item("Error", "EmptyPayload", "Error", `{}`),
		item("Error", "BatchWithoutLength", "Error", `{"batchError":true}`),
		item("XMLHttpRequest", "NoTiming", "Error", `{"method":"GET"}`),
		item("XMLHttpRequest", "WrongTypes", "Error", `{"method":5,"statusCode":"500"}`),
		item("Performance", "LCP", "OK", `{"rating":"good"}`),
		item("Performance", "NavigationTiming", "OK",
			`{"extra":{"loadEvent":"slow","dnsLookup":null,"triggerPageUrl":"/"}}`),
		item("Performance", "NavigationTiming", "OK", `{"extra":"not-an-object"}`),
		item("Performance", "ResourceTiming", "OK", `{"extra":{}}`),
		item("Performance", "ResourceList", "OK", `{"resourceList":null}`),
		item("Performance", "ResourceList", "OK", `{"resourceList":[{}]}`),
		item("Performance", "LongTask", "OK", `{"longTasks":[{}]}`),
		item("Performance", "Memory", "OK", `{"memory":{}}`),
		item("Performance", "Memory", "OK", `{}`),
		item("Click", "NoExtra", "OK", `{}`),
		item("Click", "StringExtra", "OK", `{"extra":"nope"}`),
		item("Exposure", "Exposure", "OK", `{"extra":{}}`),
		item("PV", "PageDwell", "OK", `{"extra":{}}`),
		item("", "", "", `{}`),
	}

	// A panic here would take down the whole /api/log request path, so the
	// assertion is simply that recording completes and leaves the registry sane.
	recordSentryReportBatch(malformed)

	if _, err := sentryRegistry.Gather(); err != nil {
		t.Fatalf("registry became inconsistent after malformed payloads: %v", err)
	}
}
