import { HTTPError } from "ky";
import { type ApiDataResponse, api } from "@/lib/api";

export type Kind = "Blueprint" | "Integration";

export type Ref = { kind: Kind; identifier: string };

export type DefinitionDocument = Ref & {
  title: string;
  description?: string;
  yaml: string;
};

export type Definitions = {
  revision: number;
  documents: DefinitionDocument[];
};

export type FieldChange = {
  path: string;
  before?: unknown;
  after?: unknown;
};

export type Change = Ref & {
  title: string;
  action: "add" | "change" | "remove";
  fields?: FieldChange[];
};

export type EntityName = { blueprint: string; identifier: string; title: string };

export type Impact = {
  deleted_entities: {
    blueprint?: string;
    integration?: string;
    count: number;
    examples: EntityName[];
  }[];
  cleared_values: FieldImpact[];
  dropped_links: FieldImpact[];
};

export type FieldImpact = {
  blueprint: string;
  name: string;
  count: number;
  examples: EntityName[];
};

export type Summary = { added: number; changed: number; removed: number };

export type Plan = {
  id?: string;
  expires_at?: string;
  base_revision: number;
  rollback_of?: number;
  summary: Summary;
  changes: Change[];
  impact: Impact;
};

export type PlanRequest = {
  source: string;
  partial: boolean;
  remove?: Ref[];
};

export type Problem = {
  document?: number;
  kind?: Kind;
  identifier?: string;
  path?: string;
  line?: number;
  column?: number;
  message: string;
};

export type Revision = {
  number: number;
  author: string;
  message: string;
  rollback_of?: number;
  created_at: string;
  summary: Summary;
  changes: Change[];
  impact: Impact;
  documents?: DefinitionDocument[];
};

export type RevisionPage = {
  revisions: Revision[];
  offset: number;
  limit: number;
  total: number;
};

export type Diff = { from: number; to: number; changes: Change[] };

export const revisionPageSize = 50;

export const definitionKeys = {
  all: ["definitions"] as const,
  current: ["definitions", "current"] as const,
  schema: ["definitions", "schema"] as const,
  revisions: (page: number) => ["definitions", "revisions", page] as const,
  revision: (number: number) => ["definitions", "revision", number] as const,
  diff: (from: number, to: number) => ["definitions", "diff", from, to] as const,
};

export async function getDefinitions(): Promise<Definitions> {
  return (await api.get("definitions").json<ApiDataResponse<Definitions>>()).data;
}

export async function getSchema(): Promise<object> {
  return (await api.get("definitions/schema").json<ApiDataResponse<object>>()).data;
}

export async function exportDefinitions(): Promise<{ revision: number; yaml: string }> {
  return (
    await api.get("definitions/export").json<ApiDataResponse<{ revision: number; yaml: string }>>()
  ).data;
}

export async function planDefinitions(request: PlanRequest): Promise<Plan> {
  return (await api.post("definitions/plan", { json: request }).json<ApiDataResponse<Plan>>()).data;
}

export async function planRollback(number: number): Promise<Plan> {
  return (await api.post(`definitions/revisions/${number}/rollback`).json<ApiDataResponse<Plan>>())
    .data;
}

export async function applyPlan(planId: string, message: string): Promise<Revision> {
  return (
    await api
      .post("definitions/apply", { json: { plan_id: planId, message } })
      .json<ApiDataResponse<Revision>>()
  ).data;
}

export async function getRevisions(page: number): Promise<RevisionPage> {
  const searchParams = { offset: (page - 1) * revisionPageSize, limit: revisionPageSize };
  return (
    await api.get("definitions/revisions", { searchParams }).json<ApiDataResponse<RevisionPage>>()
  ).data;
}

export async function getRevision(number: number): Promise<Revision> {
  return (await api.get(`definitions/revisions/${number}`).json<ApiDataResponse<Revision>>()).data;
}

export async function getDiff(from: number, to: number): Promise<Diff> {
  return (
    await api.get("definitions/diff", { searchParams: { from, to } }).json<ApiDataResponse<Diff>>()
  ).data;
}

type ProblemsErrorBody = { error?: { problems?: Problem[] } };

// problemsOf returns the problems the API found with planned definitions.
export function problemsOf(error: unknown): Problem[] {
  if (!(error instanceof HTTPError)) return [];
  return (error.data as ProblemsErrorBody | undefined)?.error?.problems ?? [];
}
