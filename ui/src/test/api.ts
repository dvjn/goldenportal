import { vi } from "vite-plus/test";
import type { Me } from "@/features/auth";

type Handler = () => Response | Promise<Response>;

export const alice: Me = {
  user: { id: "u-1", email: "alice@example.test", name: "Alice Archer" },
  groups: [
    { id: "g-1", name: "all-engineers" },
    { id: "g-2", name: "platform-team" },
  ],
};

export const signedIn: Record<string, Handler> = {
  "GET /api/v1/config": () => Response.json({ data: { name: "Acme" } }),
  "GET /api/v1/auth/me": () => Response.json({ data: alice }),
};

export function stubApi(handlers: Record<string, Handler>) {
  const fetchMock = vi.fn(async (request: Request) => {
    const handler = handlers[`${request.method} ${new URL(request.url).pathname}`];
    return handler
      ? handler()
      : Response.json({ error: { message: "no handler" } }, { status: 404 });
  });
  vi.stubGlobal("fetch", fetchMock);

  return fetchMock;
}
