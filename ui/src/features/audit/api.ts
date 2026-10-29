import { type ApiDataResponse, api } from "@/lib/api";

export type ActorKind = "user" | "service_token" | "ingest_token" | "scim" | "system";

export type AuditActor = {
  kind: ActorKind;
  id: string;
  name: string;
  email: string;
};

export type AuditParent = {
  kind: string;
  id: string;
  name: string;
};

export type AuditResource = AuditParent & { parent?: AuditParent };

export type AuditChange = { before: unknown; after: unknown };

export type AuditEntry = {
  id: number;
  at: string;
  actor: AuditActor;
  action: string;
  resource: AuditResource;
  details: Record<string, unknown> & { changes?: Record<string, AuditChange> };
  client: { ip: string; user_agent: string };
};

export type AuditPage = {
  entries: AuditEntry[];
  offset: number;
  limit: number;
  total: number;
};

/**
 * Filters of the audit list. Page is 1-based. Actor takes "kind:id" values.
 * Actor, actor_kind, action and resource_kind match any of their values. From
 * and to take a date (YYYY-MM-DD, to includes the whole day) or an RFC 3339
 * time.
 */
export type AuditQuery = {
  page?: number;
  pageSize?: number;
  q?: string;
  actor?: string[];
  actor_kind?: string[];
  action?: string[];
  resource_kind?: string[];
  from?: string;
  to?: string;
  sort?: "asc" | "desc";
};

export type AuditFacets = {
  actors: AuditActor[];
  actor_kinds: ActorKind[];
  actions: string[];
  resource_kinds: string[];
};

export const auditPageSize = 50;

export const auditKeys = {
  all: ["audit"] as const,
  page: (query: AuditQuery) => ["audit", "page", query] as const,
  facets: ["audit", "facets"] as const,
};

export function actorKey(actor: Pick<AuditActor, "kind" | "id">) {
  return `${actor.kind}:${actor.id}`;
}

export function auditQuery(query: number | AuditQuery): AuditQuery {
  return typeof query === "number" ? { page: query } : query;
}

function auditSearchParams(query: AuditQuery) {
  const pageSize = query.pageSize ?? auditPageSize;
  const params = new URLSearchParams({
    offset: String(((query.page ?? 1) - 1) * pageSize),
    limit: String(pageSize),
  });
  const optional: [string, string | undefined][] = [
    ["q", query.q?.trim()],
    ["actor", query.actor?.join(",")],
    ["actor_kind", query.actor_kind?.join(",")],
    ["action", query.action?.join(",")],
    ["resource_kind", query.resource_kind?.join(",")],
    ["from", query.from],
    ["to", query.to],
    ["sort", query.sort],
  ];
  for (const [key, value] of optional) {
    if (value) params.set(key, value);
  }
  return params;
}

export async function getAuditPage(query: number | AuditQuery): Promise<AuditPage> {
  const searchParams = auditSearchParams(auditQuery(query));
  return (await api.get("audit", { searchParams }).json<ApiDataResponse<AuditPage>>()).data;
}

export async function getAuditFacets(): Promise<AuditFacets> {
  return (await api.get("audit/facets").json<ApiDataResponse<AuditFacets>>()).data;
}
