import { HTTPError } from "ky";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { createApi, isUnauthorized } from "./api";

const api = createApi("http://localhost/api/v1");

function stubFetch(response: Response) {
  const fetchMock = vi.fn(async (_request: Request) => response);
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("api", () => {
  it("requests paths under the prefix as JSON", async () => {
    const fetchMock = stubFetch(Response.json({ data: { name: "GoldPort" } }));

    await expect(api.get("config").json()).resolves.toEqual({ data: { name: "GoldPort" } });

    const request = fetchMock.mock.calls[0]?.[0];
    expect(request?.url).toBe("http://localhost/api/v1/config");
    expect(request?.headers.get("Accept")).toBe("application/json");
  });

  it("uses the error envelope message for failed responses", async () => {
    stubFetch(Response.json({ error: { message: "route not registered" } }, { status: 404 }));

    const error = await api
      .get("missing")
      .json()
      .catch((caught: unknown) => caught);

    expect(error).toBeInstanceOf(HTTPError);
    expect((error as HTTPError).message).toBe("route not registered");
    expect((error as HTTPError).response.status).toBe(404);
  });

  it("keeps the default message when the body has no envelope", async () => {
    stubFetch(new Response("bad gateway", { status: 502 }));

    await expect(api.get("config").json()).rejects.toThrow(/502/);
  });
});

describe("isUnauthorized", () => {
  it("is true only for a 401 response", async () => {
    stubFetch(Response.json({ error: { message: "authentication required" } }, { status: 401 }));
    const unauthorized = await api.get("auth/me").catch((caught: unknown) => caught);
    stubFetch(Response.json({ error: { message: "failed" } }, { status: 500 }));
    const failed = await api.get("auth/me").catch((caught: unknown) => caught);

    expect(isUnauthorized(unauthorized)).toBe(true);
    expect(isUnauthorized(failed)).toBe(false);
    expect(isUnauthorized(new Error("offline"))).toBe(false);
  });
});
