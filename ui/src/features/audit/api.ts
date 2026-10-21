import { type ApiDataResponse, api } from "@/lib/api";

export type AuditEntry = {
  id: number;
  at: string;
  actor: { user_id?: string; label: string };
  action: string;
  target: { kind: string; id: string; name: string };
  details: Record<string, unknown>;
};

export type AuditPage = {
  entries: AuditEntry[];
  offset: number;
  limit: number;
  total: number;
};

export const auditPageSize = 50;

export const auditKeys = {
  page: (page: number) => ["audit", page] as const,
};

export async function getAuditPage(page: number): Promise<AuditPage> {
  const searchParams = { offset: (page - 1) * auditPageSize, limit: auditPageSize };
  return (await api.get("audit", { searchParams }).json<ApiDataResponse<AuditPage>>()).data;
}
