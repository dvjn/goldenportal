import { type ConfigDoc, configDocs, type DocSummary } from "@/features/config-docs";
import { type ApiDataResponse, api, type Page } from "@/lib/api";

export type ConnectionSpec = {
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
  // pulls, receives and checks are what this connection's connector config
  // does: sync on a schedule, take pushes, check its system.
  pulls: boolean;
  receives: boolean;
  checks: boolean;
  // events is whether what it receives are its system's events, sent to a webhook its connector
  // sets up, rather than pushes.
  events: boolean;
};

export type Connection = ConfigDoc<ConnectionSpec>;
export type ConnectionSummary = DocSummary;

export type Connector = {
  id: string;
  title: string;
  description: string;
  icon: string;
  color: string;
  // pulls, receives and checks are what the connector can do; an
  // connection's config may use less.
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
  // readSince is, for each resource a sync read only what changed of, the
  // earliest time it read from; it read the others in full.
  readSince?: Record<string, string>;
};

// ConnectionStatus is how a published connection is doing: its latest run
// and the blueprints it feeds, with how many of their entities it wrote,
// and the resources blueprints' published sources read from it.
export type ConnectionStatus = {
  connection: string;
  lastRun: Run | null;
  feeds: { blueprint: string; title: string; entities: number }[];
  sources: { resource: string; blueprint: string; title: string; mode: "reconcile" | "upsert" }[];
};

export const connections = configDocs<ConnectionSpec>("connections");

const path = (identifier: string) => `connections/${encodeURIComponent(identifier)}`;

export const connectionKeys = {
  connectors: ["connectors"] as const,
  status: ["connection-status"] as const,
  tokens: (identifier: string) => [...connections.keys.detail(identifier), "tokens"] as const,
  runs: (identifier: string) => [...connections.keys.detail(identifier), "runs"] as const,
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

// syncConnection starts syncing a connection that pulls now, in mode, or as its
// schedule would without one, and returns the run as it starts.
export async function syncConnection(identifier: string, mode?: RunMode): Promise<Run> {
  const searchParams = mode ? { mode } : undefined;
  return (await api.post(`${path(identifier)}/sync`, { searchParams }).json<ApiDataResponse<Run>>())
    .data;
}

export type CheckResult = {
  // version is what was checked: the published version, or the draft of an
  // connection that hasn't been published.
  version: "published" | "draft";
  passed: boolean;
  error?: string;
  // warning is what won't work, though the check passed.
  warning?: string;
};

// checkConnection checks the connector can reach its system with the
// current config and secrets.
export async function checkConnection(identifier: string): Promise<CheckResult> {
  return (await api.post(`${path(identifier)}/check`).json<ApiDataResponse<CheckResult>>()).data;
}

export async function listStatus(): Promise<ConnectionStatus[]> {
  return (await api.get("connection-status").json<ApiDataResponse<ConnectionStatus[]>>()).data;
}

export type RunPage = Page<Run>;

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

// ingestUrl is where systems push data for a connection.
export function ingestUrl(identifier: string) {
  return `${window.location.origin}/api/ingest/${encodeURIComponent(identifier)}`;
}
