import { vi } from "vite-plus/test";
import type { Me, Permission } from "@/features/auth";
import type { Scim, Settings } from "@/features/settings";

type Handler = (request: Request) => Response | Promise<Response>;

const allPermissions: Permission[] = [
  "settings.read",
  "access.read",
  "access.write",
  "audit.read",
  "catalog.read",
  "catalog.write",
  "catalog.model",
  "integrations.sync",
  "integrations.tokens",
];

export const alice: Me = {
  user: { id: "u-1", email: "alice@example.test", name: "Alice Archer" },
  groups: [
    { id: "g-1", name: "all-engineers" },
    { id: "g-2", name: "platform-team" },
  ],
  role: "admin",
  permissions: allPermissions,
  bootstrap: false,
};

export const cara: Me = {
  user: { id: "u-3", email: "cara@example.test", name: "Cara Cole" },
  groups: [{ id: "g-4", name: "contractors" }],
  role: "member",
  permissions: ["catalog.read"],
  bootstrap: false,
};

export const settings: Settings = {
  name: "Acme Portal",
  public_url: "https://portal.example.test",
  oidc_issuer_url: "https://login.example.test/application/o/portal/",
  oidc_client_id: "portal",
  oidc_client_secret_set: true,
  session_max_age_seconds: 36000,
  session_idle_timeout_seconds: 2700,
  bootstrap: { admins: ["alice@example.test"], admin_granted: true },
};

export const scim: Scim = {
  endpoint: "https://portal.example.test/scim/v2",
  last_push_at: new Date().toISOString(),
  users: 3,
  groups: 4,
};

export const signedIn: Record<string, Handler> = {
  "GET /api/v1/config": () => Response.json({ data: { name: "Acme" } }),
  "GET /api/v1/auth/me": () => Response.json({ data: alice }),
};

export const signedInAs = (me: Me): Record<string, Handler> => ({
  ...signedIn,
  "GET /api/v1/auth/me": () => Response.json({ data: me }),
});

export const adminApi: Record<string, Handler> = {
  ...signedIn,
  "GET /api/v1/settings": () => Response.json({ data: settings }),
  "GET /api/v1/scim": () => Response.json({ data: scim }),
};

export function stubApi(handlers: Record<string, Handler>) {
  const fetchMock = vi.fn(async (request: Request) => {
    const handler = handlers[`${request.method} ${new URL(request.url).pathname}`];
    return handler
      ? handler(request)
      : Response.json({ error: { message: "no handler" } }, { status: 404 });
  });
  vi.stubGlobal("fetch", fetchMock);

  return fetchMock;
}
