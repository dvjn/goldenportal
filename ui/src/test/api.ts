import { vi } from "vite-plus/test";
import type { Grant, Subjects, UserCheck } from "@/features/access";
import type { Me, Permission } from "@/features/auth";
import type { Scim, Settings } from "@/features/settings";

export type Handler = (request: Request) => Response | Promise<Response>;

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

export const bob: Me = {
  user: { id: "u-2", email: "bob@example.test", name: "Bob Baker" },
  groups: [{ id: "g-3", name: "payments-team" }],
  role: "maintainer",
  permissions: [
    "catalog.read",
    "catalog.write",
    "catalog.model",
    "integrations.sync",
    "integrations.tokens",
  ],
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
  "GET /api/v1/catalog/blueprints": () => Response.json({ data: { blueprints: [] } }),
};

export const signedInAs = (me: Me): Record<string, Handler> => ({
  ...signedIn,
  "GET /api/v1/auth/me": () => Response.json({ data: me }),
});

export const grants: Grant[] = [
  {
    id: "r-1",
    subject: { type: "group", id: "g-2", name: "platform-team", member_count: 1 },
    role: "admin",
    granted_by: "alice@example.test",
    created_at: "2026-10-20T09:00:00Z",
    updated_at: "2026-10-20T09:00:00Z",
    last_admin: true,
  },
  {
    id: "r-2",
    subject: { type: "group", id: "g-3", name: "payments-team", member_count: 1 },
    role: "maintainer",
    granted_by: "alice@example.test",
    created_at: "2026-10-21T09:00:00Z",
    updated_at: "2026-10-21T09:00:00Z",
    last_admin: false,
  },
];

export const subjects: Subjects = {
  groups: [
    { id: "g-1", name: "all-engineers", active: true },
    { id: "g-4", name: "contractors", active: true },
    { id: "g-3", name: "payments-team", active: true, role: "maintainer" },
    { id: "g-2", name: "platform-team", active: true, role: "admin" },
  ],
  users: [
    { id: "u-1", name: "Alice Archer", email: "alice@example.test", active: true },
    { id: "u-2", name: "Bob Baker", email: "bob@example.test", active: true },
    { id: "u-5", name: "Dan Dormant", email: "dan@example.test", active: false },
  ],
};

export const bobCheck: UserCheck = {
  user: { id: "u-2", name: "Bob Baker", email: "bob@example.test", active: true },
  role: "maintainer",
  bootstrap: false,
  carried_by: [{ type: "group", id: "g-3", name: "payments-team", role: "maintainer" }],
  groups: [
    { id: "g-1", name: "all-engineers" },
    { id: "g-3", name: "payments-team" },
  ],
  last_sign_in_at: "2026-10-22T06:00:00Z",
};

export const adminApi: Record<string, Handler> = {
  ...signedIn,
  "GET /api/v1/settings": () => Response.json({ data: settings }),
  "GET /api/v1/scim": () => Response.json({ data: scim }),
  "GET /api/v1/access/grants": () => Response.json({ data: { grants } }),
  "GET /api/v1/access/subjects": () => Response.json({ data: subjects }),
  "GET /api/v1/access/users/u-2": () => Response.json({ data: bobCheck }),
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
