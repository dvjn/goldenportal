import { type ApiDataResponse, api } from "@/lib/api";

export type Connector = "manual" | "push" | "gitlab" | "sonarqube";

export type RunStatus = "running" | "succeeded" | "partial" | "failed";

export type RunTrigger = "schedule" | "manual" | "push";

export type Run = {
  id: number;
  trigger: RunTrigger;
  actor: string;
  kind?: string;
  mode?: string;
  status: RunStatus;
  started_at: string;
  finished_at?: string;
  counts: { fetched: number; upserted: number; deleted: number; skipped: number };
  warnings: string[];
  error?: string;
};

export type IntegrationSummary = {
  identifier: string;
  title: string;
  description?: string;
  connector: Connector;
  manual: boolean;
  pull: boolean;
  interval?: string;
  entity_count: number;
  last_run?: Run;
  next_run_at?: string;
};

export type IntegrationDetail = IntegrationSummary & {
  entities: { blueprint: string; title: string; count: number }[];
  kinds: { kind: string; blueprint: string; blueprint_title: string; canonical: boolean }[];
  secrets: { name: string; variable: string }[];
  ingest_url?: string;
};

export type IngestToken = {
  id: string;
  name: string;
  created_by: string;
  created_at: string;
  expires_at: string;
  last_used_at?: string;
};

export type NewIngestToken = {
  name: string;
  expires_in_days: number;
};

export type CreatedIngestToken = {
  token: IngestToken;
  secret: string;
  ingest_url: string;
};

export const integrationKeys = {
  all: ["integrations"] as const,
  list: ["integrations", "list"] as const,
  detail: (identifier: string) => ["integrations", "detail", identifier] as const,
  runs: (identifier: string) => ["integrations", "runs", identifier] as const,
  tokens: (identifier: string) => ["integrations", "tokens", identifier] as const,
};

const integrationPath = (identifier: string) => `integrations/${encodeURIComponent(identifier)}`;

export async function getIntegrations(): Promise<IntegrationSummary[]> {
  return (
    await api.get("integrations").json<ApiDataResponse<{ integrations: IntegrationSummary[] }>>()
  ).data.integrations;
}

export async function getIntegration(identifier: string): Promise<IntegrationDetail> {
  return (await api.get(integrationPath(identifier)).json<ApiDataResponse<IntegrationDetail>>())
    .data;
}

export async function getRuns(identifier: string): Promise<Run[]> {
  return (
    await api.get(`${integrationPath(identifier)}/runs`).json<ApiDataResponse<{ runs: Run[] }>>()
  ).data.runs;
}

export async function syncIntegration(identifier: string): Promise<Run> {
  return (await api.post(`${integrationPath(identifier)}/sync`).json<ApiDataResponse<Run>>()).data;
}

export async function getIngestTokens(identifier: string): Promise<IngestToken[]> {
  return (
    await api
      .get(`${integrationPath(identifier)}/tokens`)
      .json<ApiDataResponse<{ tokens: IngestToken[] }>>()
  ).data.tokens;
}

export async function createIngestToken(
  identifier: string,
  token: NewIngestToken,
): Promise<CreatedIngestToken> {
  return (
    await api
      .post(`${integrationPath(identifier)}/tokens`, { json: token })
      .json<ApiDataResponse<CreatedIngestToken>>()
  ).data;
}

export async function revokeIngestToken(identifier: string, id: string) {
  await api.delete(`${integrationPath(identifier)}/tokens/${encodeURIComponent(id)}`);
}
