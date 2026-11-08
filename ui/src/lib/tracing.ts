import type { MutationCacheConfig } from "@tanstack/react-query";
import type { AnyRouter } from "@tanstack/react-router";

// TracingConfig is the server asking for browser traces, and which share of
// them to keep.
export type TracingConfig = { sampleRatio: number };

// Tracing is what the tracing chunk does once it has loaded.
export type Tracing = {
  fetch: typeof fetch;
  startAction(mutation: object): void;
  endAction(mutation: object, failed: boolean): void;
};

// Settled is when a route load finished, and whether a loader failed.
export type Settled = { at: number; failed: boolean };

export function settled(router: AnyRouter): Settled {
  return {
    at: performance.now(),
    failed: router.state.matches.some((match) => match.status === "error"),
  };
}

let tracing: Tracing | undefined;

// tracedFetch is how the API client reaches the network, so that each
// attempt, retries included, is a span once tracing is on.
export const tracedFetch: typeof fetch = (input, init) =>
  tracing ? tracing.fetch(input, init) : fetch(input, init);

// mutationTracing makes every mutation a user action, the parent of the
// requests it makes.
export const mutationTracing: MutationCacheConfig = {
  onMutate: (_variables, mutation) => tracing?.startAction(mutation),
  onSettled: (_data, error, _variables, _result, mutation) =>
    tracing?.endAction(mutation, error !== null),
};

// startTracing loads the tracing chunk only when the config asks for traces,
// so the UI downloads and sends nothing more without. The first page load
// can finish before the chunk arrives, so its end is noted from the start.
// Tracing is best effort: a config that fails to load is the root route's
// to show, and a chunk that fails to load leaves the app untraced.
export function startTracing(router: AnyRouter, config: Promise<{ tracing?: TracingConfig }>) {
  const firstLoad = new Promise<Settled>((resolve) => {
    const stop = router.subscribe("onResolved", () => {
      stop();
      resolve(settled(router));
    });
  });
  return config
    .then(async ({ tracing: options }) => {
      if (!options) return;
      const { createTracing } = await import("./tracing-sdk");
      tracing = createTracing(options, router, firstLoad);
    })
    .catch(() => undefined);
}
