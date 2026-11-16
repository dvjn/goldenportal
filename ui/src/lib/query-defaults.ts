import type { DefaultOptions } from "@tanstack/react-query";

// queryDefaults are the app's query options, which tests share.
export const queryDefaults = {
  queries: {
    // The API client already retries what can pass (network errors, 5xx and
    // 429); retrying here again would multiply those and delay every error.
    retry: false,
    // A loader fetches what its page reads just before the page mounts, so
    // data that fresh isn't fetched again; changes invalidate what they touch.
    staleTime: 10_000,
  },
} satisfies DefaultOptions;
