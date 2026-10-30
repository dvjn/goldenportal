import { type ConfigDoc, configDocs, type DocSummary } from "@/features/config-docs";
import { type ApiDataResponse, api } from "@/lib/api";

export type Field = { identifier: string; expression: string };

export type Mapping = {
  // resource is what a pull connector fetches for the mapping, such as users.
  resource?: string;
  blueprint: string;
  items: string;
  filter?: string;
  entity: { identifier: string; title?: string; properties: Field[] };
};

export type IntegrationSpec = {
  identifier: string;
  title: string;
  description?: string;
  icon?: string;
  connector: { type: string; config?: Record<string, unknown> };
  sync?: { interval: string };
  // mappings are also the only blueprints the integration writes.
  mappings: Mapping[];
};

export type Integration = ConfigDoc<IntegrationSpec>;
export type IntegrationSummary = DocSummary;

export type Connector = {
  id: string;
  title: string;
  description: string;
  mode: "push" | "pull";
  resources?: string[];
};

export type IngestionToken = {
  id: string;
  name: string;
  prefix: string;
  createdBy: string;
  createdAt: string;
  lastUsedAt: string | null;
};

export type CreatedIngestionToken = IngestionToken & { secret: string };

export type RunError = {
  index: number;
  blueprint?: string;
  identifier?: string;
  problems: string[];
};

export type Run = {
  id: number;
  startedAt: string;
  finishedAt: string;
  trigger: "push" | "schedule" | "manual";
  token?: string;
  received: number;
  created: number;
  updated: number;
  unchanged: number;
  deleted: number;
  failed: number;
  errors: RunError[];
  // error is why a pull sync fetched nothing, which left its entities as they were.
  error?: string;
};

export const integrations = configDocs<IntegrationSpec>("integrations");

const path = (identifier: string) => `integrations/${encodeURIComponent(identifier)}`;

export const integrationKeys = {
  connectors: ["connectors"] as const,
  tokens: (identifier: string) => [...integrations.keys.detail(identifier), "tokens"] as const,
  runs: (identifier: string) => [...integrations.keys.detail(identifier), "runs"] as const,
};

export async function listConnectors(): Promise<Connector[]> {
  return (await api.get("connectors").json<ApiDataResponse<Connector[]>>()).data;
}

export async function listTokens(identifier: string): Promise<IngestionToken[]> {
  return (await api.get(`${path(identifier)}/tokens`).json<ApiDataResponse<IngestionToken[]>>())
    .data;
}

export async function createToken(
  identifier: string,
  name: string,
): Promise<CreatedIngestionToken> {
  return (
    await api
      .post(`${path(identifier)}/tokens`, { json: { name } })
      .json<ApiDataResponse<CreatedIngestionToken>>()
  ).data;
}

export async function revokeToken(identifier: string, id: string): Promise<void> {
  await api.delete(`${path(identifier)}/tokens/${id}`);
}

// syncIntegration syncs a pull integration now.
export async function syncIntegration(identifier: string): Promise<Run> {
  return (await api.post(`${path(identifier)}/sync`).json<ApiDataResponse<Run>>()).data;
}

export async function listRuns(identifier: string): Promise<Run[]> {
  return (await api.get(`${path(identifier)}/runs`).json<ApiDataResponse<Run[]>>()).data;
}

// ingestUrl is where systems push data for an integration.
export function ingestUrl(identifier: string) {
  return `${window.location.origin}/api/v1/ingest/${encodeURIComponent(identifier)}`;
}
