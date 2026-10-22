import { QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { createTestQueryClient } from "@/test/render";
import { configQueryOptions, useConfig } from "./queries";

vi.mock("@/lib/api", async (importOriginal) => {
  const { createApi } = await importOriginal<typeof import("@/lib/api")>();
  return { api: createApi("http://localhost/api/v1") };
});

function stubFetch(respond: () => Response) {
  const fetchMock = vi.fn(async (_request: Request) => respond());
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("configQueryOptions", () => {
  it("loads the portal config", async () => {
    const fetchMock = stubFetch(() => Response.json({ data: { name: "Acme" } }));

    const config = await createTestQueryClient().fetchQuery(configQueryOptions);

    expect(config).toEqual({ name: "Acme" });
    expect(fetchMock.mock.calls[0]?.[0].url).toBe("http://localhost/api/v1/config");
  });

  it("reports a failed request", async () => {
    stubFetch(() => Response.json({ error: { message: "config unavailable" } }, { status: 500 }));

    await expect(createTestQueryClient().fetchQuery(configQueryOptions)).rejects.toThrow(
      "Config unavailable",
    );
  });
});

describe("useConfig", () => {
  it("returns the loaded config", () => {
    const client = createTestQueryClient();
    client.setQueryData(configQueryOptions.queryKey, { name: "Acme" });
    const wrapper = ({ children }: Readonly<{ children: ReactNode }>) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );

    const { result } = renderHook(() => useConfig(), { wrapper });

    expect(result.current).toEqual({ name: "Acme" });
  });
});
