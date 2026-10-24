import { vi } from "vite-plus/test";

type Handler = (request: Request) => Response | Promise<Response>;

// stubApi answers API requests by "METHOD path", where path is relative to
// /api/v1, e.g. "GET me" or "POST auth/login". Unstubbed requests fail loudly.
// It returns the requests it saw, keyed the same way, with readable bodies.
export function stubApi(routes: Record<string, Handler>) {
  const requests: Record<string, Request> = {};
  const fetchMock = vi.fn(async (request: Request) => {
    const path = new URL(request.url).pathname.replace(/^\/api\/v1\//, "");
    requests[`${request.method} ${path}`] = request.clone();
    const handler = routes[`${request.method} ${path}`];
    if (!handler) {
      return Response.json(
        { error: { message: `no stub for ${request.method} ${path}` } },
        { status: 500 },
      );
    }
    return handler(request);
  });
  vi.stubGlobal("fetch", fetchMock);
  return requests;
}

export function ok(data: unknown) {
  return () => Response.json({ data });
}

export function fail(status: number, message: string) {
  return () => Response.json({ error: { message } }, { status });
}

export const acmeConfig = ok({ name: "Acme" });

export const ada = {
  id: "1",
  email: "ada@example.com",
  displayName: "Ada Lovelace",
  role: "admin",
};
