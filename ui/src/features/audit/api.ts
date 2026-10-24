import { type ApiDataResponse, api } from "@/lib/api";

export type AuditEvent = {
  id: number;
  at: string;
  actor: { id?: string; label: string };
  action: string;
  target: { type: string; id: string; label: string };
  details: Record<string, unknown>;
};

export type AuditPage = {
  events: AuditEvent[];
  // total is how many events match the filters, across every page.
  total: number;
};

// AuditFilters picks a page of events. Dates are YYYY-MM-DD; to includes that day.
export type AuditFilters = {
  q?: string;
  actions?: string[];
  from?: string;
  to?: string;
  page?: number;
  size?: number;
};

// auditActions names every action the audit log records, in the order filters list them.
export const auditActions: Record<string, string> = {
  "auth.sign_in": "Signed in",
  "auth.sign_out": "Signed out",
  "token.create": "Created API token",
  "token.revoke": "Revoked API token",
  "access.grant": "Granted access",
  "access.revoke": "Removed access",
  "access.bootstrap_admin": "Made bootstrap admin",
};

export const auditKeys = {
  all: ["audit"] as const,
  page: (filters: AuditFilters) => [...auditKeys.all, filters] as const,
};

export async function listAuditEvents(filters: AuditFilters): Promise<AuditPage> {
  const searchParams = new URLSearchParams();
  if (filters.q) searchParams.set("q", filters.q);
  if (filters.actions?.length) searchParams.set("action", filters.actions.join(","));
  if (filters.from) searchParams.set("from", filters.from);
  if (filters.to) searchParams.set("to", filters.to);
  if (filters.page) searchParams.set("page", String(filters.page));
  if (filters.size) searchParams.set("size", String(filters.size));
  return (await api.get("audit", { searchParams }).json<ApiDataResponse<AuditPage>>()).data;
}
