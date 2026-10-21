import { QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { navigateAway } from "@/lib/navigation";
import { alice, signedIn, stubApi } from "@/test/api";
import { createTestQueryClient } from "@/test/render";
import { loginUrl } from "./api";
import { meQueryOptions, useLogout, useMe } from "./queries";

vi.mock("@/lib/api", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/lib/api")>();
  return { ...original, api: original.createApi("http://localhost/api/v1") };
});

vi.mock("@/lib/navigation", () => ({ navigateAway: vi.fn() }));

afterEach(() => {
  vi.unstubAllGlobals();
  vi.mocked(navigateAway).mockClear();
});

function wrapperFor(client = createTestQueryClient()) {
  return ({ children }: Readonly<{ children: ReactNode }>) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

describe("meQueryOptions", () => {
  it("loads the signed-in user", async () => {
    stubApi(signedIn);

    await expect(createTestQueryClient().fetchQuery(meQueryOptions)).resolves.toEqual(alice);
  });
});

describe("useMe", () => {
  it("returns the loaded user", () => {
    const client = createTestQueryClient();
    client.setQueryData(meQueryOptions.queryKey, alice);

    const { result } = renderHook(() => useMe(), { wrapper: wrapperFor(client) });

    expect(result.current).toEqual(alice);
  });
});

describe("useLogout", () => {
  it("signs out and follows the provider logout", async () => {
    const fetchMock = stubApi({
      "POST /api/v1/auth/logout": () =>
        Response.json({ data: { redirect_to: "http://login.example.test/end" } }, { status: 201 }),
    });
    const { result } = renderHook(() => useLogout(), { wrapper: wrapperFor() });

    act(() => result.current.mutate());

    await waitFor(() => expect(navigateAway).toHaveBeenCalledWith("http://login.example.test/end"));
    expect(fetchMock.mock.calls[0]?.[0].method).toBe("POST");
  });
});

describe("loginUrl", () => {
  it("carries the page to return to", () => {
    expect(loginUrl("/catalog?tab=owned")).toBe(
      "/api/v1/auth/login?return_to=%2Fcatalog%3Ftab%3Downed",
    );
  });
});
