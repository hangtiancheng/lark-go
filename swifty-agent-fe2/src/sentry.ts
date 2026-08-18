import { enablePlugin, init, isInitialized } from "@swifty.js/sentry";
import { ExposurePlugin, PerformancePlugin } from "@swifty.js/sentry/plugins";

// dsn goes through the Vite dev proxy (/api → Go backend :8123), which
// converts the reports into Prometheus metrics at GET /api/metrics.
//
// Oversized single events (rrweb ScreenRecord payloads, ResourceList on dev
// pages that load hundreds of modules) push the batch past the 64KB
// fetch-keepalive body limit, permanently wedging every retry — drop them.
const MAX_EVENT_BYTES = 50 * 1024;

// Framework-agnostic setup: unlike the React app there is no error-boundary
// wrapper — the SDK's global error/unhandledrejection capture covers Lit.
export function setupSentry() {
  if (isInitialized()) return;
  init({
    dsn: "/api/log",
    projectId: "swifty-agent-fe2",
    beforePushEventList: (eventList) =>
      eventList.filter(
        (item) => JSON.stringify(item).length <= MAX_EVENT_BYTES,
      ),
  });
  enablePlugin(new PerformancePlugin(), new ExposurePlugin());
}
