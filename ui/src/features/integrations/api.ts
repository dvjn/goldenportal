import { type ApiDataResponse, api } from "@/lib/api";

export type Connector = "manual" | "push" | "gitlab" | "sonarqube" | "static";

/** The icon an integration shows: its definition's icon, or its connector's. */
export type IntegrationIcon = "gitlab" | "sonarqube" | "database" | "upload" | "pencil" | "webhook";

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

/** One kind the integration reads and the blueprint it feeds. */
export type IntegrationKind = {
  kind: string;
  blueprint: string;
  blueprint_title: string;
  canonical: boolean;
};

export type IntegrationSummary = {
  identifier: string;
  title: string;
  description?: string;
  connector: Connector;
  icon: IntegrationIcon;
  manual: boolean;
  pull: boolean;
  interval?: string;
  /** The defining document, such as "Integration/gitlab"; absent for the manual integration. */
  document?: string;
  kinds: IntegrationKind[];
  entity_count: number;
  last_run?: Run;
  next_run_at?: string;
};

/** A secret the connector reads; present says whether the server has it, never its value. */
export type IntegrationSecret = { name: string; variable: string; present: boolean };

/** The jq expression for one property or relation, with the target field's type. */
export type FieldMapping = {
  key: string;
  title: string;
  expression: string;
  /** The property type, or "relation". */
  type: string;
  format?: string;
  /** The blueprint a relation links to. */
  target?: string;
  many?: boolean;
};

export type IntegrationMapping = {
  kind: string;
  blueprint: string;
  blueprint_title: string;
  canonical: boolean;
  /** Blueprints bound as jq variables, $ plus the identifier with hyphens as underscores. */
  lookups: string[];
  identifier?: string;
  title?: string;
  properties: FieldMapping[];
  relations: FieldMapping[];
};

export type IntegrationDetail = IntegrationSummary & {
  entities: { blueprint: string; title: string; count: number }[];
  /** The connector's settings as written in the definition; they hold no secrets. */
  config: Record<string, unknown>;
  secrets: IntegrationSecret[];
  mappings: IntegrationMapping[];
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
