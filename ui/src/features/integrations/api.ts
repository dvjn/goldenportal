import { type ConfigDoc, configDocs, type DocSummary } from "@/features/config-docs";
import { type ApiDataResponse, api } from "@/lib/api";

export type Field = { identifier: string; expression: string };

export type Mapping = {
  // resource names the items the mapping reads, such as users.
  resource: string;
  blueprint: string;
  filter?: string;
  entity: { identifier: string; title?: string; properties: Field[] };
};

export type IntegrationSpec = {
  identifier: string;
  title: string;
  description?: string;
  // icon and color are the connector's.
  icon?: string;
  color?: string;
  connector: { type: string; config?: Record<string, unknown> };
  // complete is how often a sync is complete, deleting what neither it nor the complete sync
  // before it saw; without it, every sync is.
  sync?: { interval: string; complete?: string };
  // mappings are also the only blueprints the integration writes.
  mappings: Mapping[];
  // pulls, receives and checks are what this integration's connector config
  // does: sync on a schedule, take pushes, check its system.
  pulls: boolean;
  receives: boolean;
  checks: boolean;
  // events is whether what it receives are its system's events, sent to a webhook its connector
  // sets up, rather than pushes.
  events: boolean;
};

export type Integration = ConfigDoc<IntegrationSpec>;
export type IntegrationSummary = DocSummary;

export type Connector = {
  id: string;
  title: string;
  description: string;
  icon: string;
  color: string;
  // pulls, receives and checks are what the connector can do; an
  // integration's config may use less.
  pulls: boolean;
  receives: boolean;
  checks: boolean;
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

// RunMode is whether a run deleted what neither it nor the complete run before it saw, or only
// what it was told was deleted.
export type RunMode = "complete" | "incremental";

export type Run = {
  id: number;
  startedAt: string;
  // finishedAt is null while a sync is running.
  finishedAt: string | null;
  trigger: "push" | "schedule" | "manual" | "action";
  mode: RunMode;
  token?: string;
  // actionRun and action are the action run behind an action trigger and its title.
  actionRun?: string;
  action?: string;
  received: number;
  created: number;
  updated: number;
  unchanged: number;
  deleted: number;
  failed: number;
  errors: RunError[];
  // error is why a sync couldn't fetch some or all of its resources; it
  // deleted nothing those resources write.
  error?: string;
  // warning is what went wrong without failing anything the run read.
  warning?: string;
};

// IntegrationStatus is how a published integration is doing: its latest run
// and the blueprints it feeds, with how many of their entities it wrote.
export type IntegrationStatus = {
  integration: string;
  lastRun: Run | null;
  feeds: { blueprint: string; title: string; entities: number }[];
};

export const integrations = configDocs<IntegrationSpec>("integrations");

const path = (identifier: string) => `integrations/${encodeURIComponent(identifier)}`;

export const integrationKeys = {
  connectors: ["connectors"] as const,
  status: ["integration-status"] as const,
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

// syncIntegration starts syncing an integration that pulls now, in mode, or as its
// schedule would without one, and returns the run as it starts.
export async function syncIntegration(identifier: string, mode?: RunMode): Promise<Run> {
  const searchParams = mode ? { mode } : undefined;
  return (await api.post(`${path(identifier)}/sync`, { searchParams }).json<ApiDataResponse<Run>>())
    .data;
}

export type CheckResult = {
  // version is what was checked: the published version, or the draft of an
  // integration that hasn't been published.
  version: "published" | "draft";
  passed: boolean;
  error?: string;
  // warning is what won't work, though the check passed.
  warning?: string;
};

// checkIntegration checks the connector can reach its system with the
// current config and secrets.
export async function checkIntegration(identifier: string): Promise<CheckResult> {
  return (await api.post(`${path(identifier)}/check`).json<ApiDataResponse<CheckResult>>()).data;
}

export async function listStatus(): Promise<IntegrationStatus[]> {
  return (await api.get("integration-status").json<ApiDataResponse<IntegrationStatus[]>>()).data;
}

export type RunPage = {
  runs: Run[];
  // total is how many runs match the filters, across every page.
  total: number;
};

export type RunTrigger = Run["trigger"];
export type RunOutcome = "running" | "succeeded" | "warnings" | "failed";

export type RunFilters = {
  q?: string;
  triggers?: RunTrigger[];
  outcomes?: RunOutcome[];
  page?: number;
  size?: number;
};

export async function listRuns(identifier: string, filters: RunFilters): Promise<RunPage> {
  const searchParams = new URLSearchParams();
  if (filters.q) searchParams.set("q", filters.q);
  if (filters.triggers?.length) searchParams.set("trigger", filters.triggers.join(","));
  if (filters.outcomes?.length) searchParams.set("outcome", filters.outcomes.join(","));
  if (filters.page) searchParams.set("page", String(filters.page));
  if (filters.size) searchParams.set("size", String(filters.size));
  return (
    await api.get(`${path(identifier)}/runs`, { searchParams }).json<ApiDataResponse<RunPage>>()
  ).data;
}

// ingestUrl is where systems push data for an integration.
export function ingestUrl(identifier: string) {
  return `${window.location.origin}/api/ingest/${encodeURIComponent(identifier)}`;
}
