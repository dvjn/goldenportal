import { QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { createTestQueryClient } from "@/test/render";
import { useConfig } from "./queries";

vi.mock("@/lib/api", async (importOriginal) => {
  const { createApi } = await importOriginal<typeof import("@/lib/api")>();
  return { api: createApi("http://localhost/api/v1") };
});

function renderConfig(respond: () => Response) {
  const fetchMock = vi.fn(async (_request: Request) => respond());
  vi.stubGlobal("fetch", fetchMock);
  const client = createTestQueryClient();
  const wrapper = ({ children }: Readonly<{ children: ReactNode }>) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return { fetchMock, ...renderHook(() => useConfig(), { wrapper }) };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useConfig", () => {
  it("loads the portal config", async () => {
    const { fetchMock, result } = renderConfig(() => Response.json({ data: { name: "Acme" } }));

    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    expect(result.current.data).toEqual({ name: "Acme" });
    expect(fetchMock.mock.calls[0]?.[0].url).toBe("http://localhost/api/v1/config");
  });

  it("reports a failed request", async () => {
    const { result } = renderConfig(() =>
      Response.json({ error: { message: "config unavailable" } }, { status: 500 }),
    );

    await waitFor(() => expect(result.current.isError).toBe(true), { timeout: 5000 });

    expect(result.current.error?.message).toBe("config unavailable");
  });
});
