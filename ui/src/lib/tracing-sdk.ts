import { ROOT_CONTEXT, type Span, SpanKind, SpanStatusCode, trace } from "@opentelemetry/api";
import { ExportResultCode, W3CTraceContextPropagator } from "@opentelemetry/core";
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-http";
import {
  BatchSpanProcessor,
  ParentBasedSampler,
  type SpanExporter,
  TraceIdRatioBasedSampler,
  TracerProvider,
} from "@opentelemetry/sdk-trace";
import type { AnyRouter } from "@tanstack/react-router";
import { webApi } from "./api";
import { type Settled, settled, type Tracing, type TracingConfig } from "./tracing";

const propagator = new W3CTraceContextPropagator();

// Page load events are when the document arrived and finished loading, as
// the browser timed them before tracing could start.
const documentTimings = ["responseEnd", "domContentLoadedEventEnd", "loadEventEnd"] as const;

// routeTemplate names a page by its route, like /catalog/$blueprint, rather
// than by its path, which carries ids.
function routeTemplate(router: AnyRouter, pathname: string) {
  const [, , route] = router.getMatchedRoutes(pathname);
  return route?.fullPath ?? "*";
}

// signedInOnly drops spans while the user is signed out, which the server
// would turn away, rather than sending each batch only to be refused.
function signedInOnly(exporter: SpanExporter, signedIn: () => boolean): SpanExporter {
  return {
    export: (spans, done) =>
      signedIn() ? exporter.export(spans, done) : done({ code: ExportResultCode.SUCCESS }),
    shutdown: () => exporter.shutdown(),
    forceFlush: () => exporter.forceFlush?.() ?? Promise.resolve(),
  };
}

function randomHex(bytes: number) {
  return Array.from(crypto.getRandomValues(new Uint8Array(bytes)), (b) =>
    b.toString(16).padStart(2, "0"),
  ).join("");
}

// serverExporter sends spans to the server. Each export says it's part of a
// trace that isn't sampled, so the server doesn't trace taking the spans in,
// which would make a trace of its own for every batch.
function serverExporter() {
  return new OTLPTraceExporter({
    url: new URL(`${webApi}/traces`, location.href).href,
    headers: async () => ({
      "X-GP-CSRF": "1",
      traceparent: `00-${randomHex(16)}-${randomHex(8)}-00`,
    }),
  });
}

// createTracing traces what the user does: the page load, each navigation
// and each action, and the API requests each makes, which carry the trace
// on to the server. Every interaction starts a trace of its own.
export function createTracing(
  config: TracingConfig,
  router: AnyRouter,
  firstLoad: Promise<Settled>,
  exporter: SpanExporter = serverExporter(),
) {
  let signedIn = true;
  const processor = new BatchSpanProcessor({ exporter: signedInOnly(exporter, () => signedIn) });
  const tracer = new TracerProvider({
    sampler: new ParentBasedSampler({ root: new TraceIdRatioBasedSampler(config.sampleRatio) }),
    spanProcessors: [processor],
  }).getTracer("goldenportal-ui");

  // Browsers don't carry the active span across awaits, so a request's
  // parent is the most recent interaction still under way.
  const interactions: Span[] = [];
  const begin = (name: string, startTime?: number) => {
    const span = tracer.startSpan(name, { root: true, startTime });
    interactions.push(span);
    return span;
  };
  const finish = (span: Span, failed: boolean, endTime?: number) => {
    const open = interactions.indexOf(span);
    if (open !== -1) interactions.splice(open, 1);
    if (failed) span.setStatus({ code: SpanStatusCode.ERROR });
    span.end(endTime);
  };

  const pageLoad = begin(`load ${routeTemplate(router, location.pathname)}`, 0);
  const [page] = performance.getEntriesByType("navigation") as PerformanceNavigationTiming[];
  for (const timing of documentTimings) {
    if (page?.[timing]) pageLoad.addEvent(timing, page[timing]);
  }
  void firstLoad.then(({ at, failed }) => finish(pageLoad, failed, at));

  // A navigation lasts until its loaders settle, or until another replaces it.
  let navigation: Span | undefined;
  router.subscribe("onBeforeNavigate", (event) => {
    if (navigation) finish(navigation, false);
    navigation = begin(`navigate ${routeTemplate(router, event.toLocation.pathname)}`);
  });
  router.subscribe("onResolved", () => {
    if (!navigation) return;
    const { at, failed } = settled(router);
    finish(navigation, failed, at);
    navigation = undefined;
  });

  const actions = new Map<object, Span>();

  return {
    async fetch(input, init) {
      // The API client hands over a request of its own for each attempt, so
      // the trace header goes on that.
      const request = input instanceof Request ? input : new Request(input, init);
      const parent = interactions.at(-1);
      const span = tracer.startSpan(
        request.method,
        {
          kind: SpanKind.CLIENT,
          attributes: {
            "http.request.method": request.method,
            "url.path": new URL(request.url).pathname,
          },
        },
        parent ? trace.setSpan(ROOT_CONTEXT, parent) : ROOT_CONTEXT,
      );
      propagator.inject(trace.setSpan(ROOT_CONTEXT, span), request.headers, {
        set: (headers, key, value) => headers.set(key, value),
      });
      try {
        const response = await fetch(request, request === input ? init : undefined);
        span.setAttribute("http.response.status_code", response.status);
        if (response.status >= 400) {
          span.setAttribute("error.type", String(response.status));
          span.setStatus({ code: SpanStatusCode.ERROR });
        }
        if (response.status === 401) signedIn = false;
        else if (response.ok) signedIn = true;
        return response;
      } catch (error) {
        span.setAttribute("error.type", error instanceof Error ? error.name : "_OTHER");
        span.setStatus({ code: SpanStatusCode.ERROR });
        throw error;
      } finally {
        span.end();
      }
    },
    startAction(mutation) {
      actions.set(
        mutation,
        begin(`action ${routeTemplate(router, router.state.location.pathname)}`),
      );
    },
    endAction(mutation, failed) {
      const span = actions.get(mutation);
      if (!span) return;
      actions.delete(mutation);
      finish(span, failed);
    },
    flush: () => processor.forceFlush(),
  } satisfies Tracing & { flush(): Promise<void> };
}
