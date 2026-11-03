import { HTTPError } from "ky";
import { afterEach, describe, expect, it, vi } from "vite-plus/test";
import { apiProblems, createApi } from "./api";

const api = createApi("http://localhost/api/web");

function stubFetch(response: Response) {
  const fetchMock = vi.fn(async (_request: Request) => response.clone());
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("api", () => {
  it("requests paths under the prefix as JSON", async () => {
    const fetchMock = stubFetch(Response.json({ data: { name: "Golden Portal" } }));

    await expect(api.get("config").json()).resolves.toEqual({ data: { name: "Golden Portal" } });

    const request = fetchMock.mock.calls[0]?.[0];
    expect(request?.url).toBe("http://localhost/api/web/config");
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

  it("reads the problems by field from a turned down request", async () => {
    const problems = { reason: ["is shorter than 5 characters"], "": ["there's no input x"] };
    stubFetch(Response.json({ error: { message: "check the inputs", problems } }, { status: 422 }));

    const error = await api
      .post("actions/restart/runs")
      .json()
      .catch((caught: unknown) => caught);

    expect(apiProblems(error)).toEqual(problems);
    expect(apiProblems(new Error("offline"))).toBeUndefined();
  });
});
