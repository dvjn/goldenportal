import type { InMemorySpanExporter, ReadableSpan } from "@opentelemetry/sdk-trace";
import { MutationCache, MutationObserver, QueryClient } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";

const sdk = vi.hoisted(() => ({
  imported: false,
  exporter: undefined as InMemorySpanExporter | undefined,
  tracing: undefined as { flush(): Promise<void> } | undefined,
}));

// The tracing chunk sends its spans to memory here, and notes it was loaded.
vi.mock("./tracing-sdk", async (importOriginal) => {
  const { createTracing } = await importOriginal<typeof import("./tracing-sdk")>();
  const { InMemorySpanExporter } = await import("@opentelemetry/sdk-trace");
  sdk.imported = true;
  return {
    createTracing: (...[config, router, firstLoad]: Parameters<typeof createTracing>) => {
      sdk.exporter = new InMemorySpanExporter();
      const tracing = createTracing(config, router, firstLoad, sdk.exporter);
      sdk.tracing = tracing;
      return tracing;
    },
  };
});

const traceparent = /^00-([0-9a-f]{32})-([0-9a-f]{16})-01$/;

function stubFetch(respond: (request: Request) => Response = () => Response.json({ data: {} })) {
  const fetchMock = vi.fn(async (request: Request) => respond(request));
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

// load imports the API client and tracing afresh, as a tab opened at path
// whose config says tracing. As in the app, the first route load starts
// before the config arrives, and its loader's request goes out after.
async function load(path: string, tracing?: { sampleRatio: number }) {
  vi.resetModules();
  sdk.imported = false;
  window.history.replaceState(null, "", path);
  const { createApi } = await import("./api");
  const { mutationTracing, startTracing } = await import("./tracing");
  const api = createApi("http://localhost/api/web");

  let configLoaded = () => {};
  const config = new Promise<void>((resolve) => {
    configLoaded = resolve;
  });
  const root = createRootRoute();
  const thing = createRoute({
    getParentRoute: () => root,
    path: "/things/$thing",
    loader: async ({ params }) => {
      await config;
      return api.get(`things/${params.thing}`).json();
    },
  });
  const router = createRouter({
    routeTree: root.addChildren([thing]),
    history: createMemoryHistory({ initialEntries: [path] }),
  });
  const queryClient = new QueryClient({ mutationCache: new MutationCache(mutationTracing) });

  const firstLoad = router.load();
  await startTracing(router, Promise.resolve({ tracing }));
  configLoaded();
  await firstLoad;
  return { api, router, queryClient };
}

async function finishedSpans() {
  await sdk.tracing?.flush();
  return sdk.exporter?.getFinishedSpans() ?? [];
}

function named(spans: ReadableSpan[], name: string) {
  const span = spans.find((each) => each.name === name);
  if (!span) throw new Error(`no span ${name} in ${spans.map((each) => each.name).join(", ")}`);
  return span;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("tracing", () => {
  it("loads nothing when the config doesn't ask for traces", async () => {
    const fetchMock = stubFetch();
    const { api } = await load("/things/a");

    await api.get("config").json();

    expect(sdk.imported).toBe(false);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    for (const [request] of fetchMock.mock.calls) {
      expect(request.headers.has("traceparent")).toBe(false);
    }
  });

  it("carries the trace on to the API with each request", async () => {
    const fetchMock = stubFetch();
    const { api } = await load("/", { sampleRatio: 1 });

    await api.get("things/a?secret=x").json();

    const header = fetchMock.mock.calls[0]?.[0].headers.get("traceparent") ?? "";
    const [, traceId, spanId] = traceparent.exec(header) ?? [];
    const request = named(await finishedSpans(), "GET");
    expect(request.spanContext()).toMatchObject({ traceId, spanId });
    expect(request.attributes).toEqual({
      "http.request.method": "GET",
      "url.path": "/api/web/things/a",
      "http.response.status_code": 200,
    });
  });

  it("parents requests to the page load, navigation or action that made them", async () => {
    stubFetch();
    const { api, router, queryClient } = await load("/things/a", { sampleRatio: 1 });

    await router.navigate({ href: "/things/b" });
    await new MutationObserver(queryClient, {
      mutationFn: () => api.post("things/b/restart").json(),
    }).mutate();

    const spans = await finishedSpans();
    const requests = spans.filter((span) => span.name === "GET" || span.name === "POST");
    const parents = requests.map((request) => {
      const parentId = request.parentSpanContext?.spanId;
      return spans.find((span) => span.spanContext().spanId === parentId)?.name;
    });
    expect(parents).toEqual([
      "load /things/$thing",
      "navigate /things/$thing",
      "action /things/$thing",
    ]);
  });

  it("marks failed requests and drops spans while signed out", async () => {
    stubFetch(() => Response.json({}, { status: 404 }));
    const { api } = await load("/", { sampleRatio: 1 });

    await api
      .get("things/missing")
      .json()
      .catch(() => undefined);
    expect(named(await finishedSpans(), "GET").attributes).toMatchObject({
      "http.response.status_code": 404,
      "error.type": "404",
    });

    sdk.exporter?.reset();
    stubFetch(() => Response.json({}, { status: 401 }));
    await api
      .get("auth/me")
      .json()
      .catch(() => undefined);
    expect(await finishedSpans()).toEqual([]);
  });
});
