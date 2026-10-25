import { queryOptions } from "@tanstack/react-query";
import type { Filter } from "@/features/catalog";
import { type ConfigDoc, configDocs, type DocSummary } from "@/features/config-docs";
import { type ApiDataResponse, api } from "@/lib/api";

export type Criterion = {
  identifier: string;
  title: string;
  description?: string;
  check: string;
  when?: string;
  fix?: string;
};

export type Tier = { name: string; title: string; color?: string; criteria: Criterion[] };

export type ScorecardSpec = {
  identifier: string;
  title: string;
  description?: string;
  icon?: string;
  blueprint: string;
  filter?: Filter;
  // tiers are a ladder, lowest first.
  tiers: Tier[];
};

export type Scorecard = ConfigDoc<ScorecardSpec>;
export type ScorecardSummary = DocSummary;

export type Status = "passed" | "failed" | "skipped" | "errored";

export type Result = {
  // tier is the highest tier reached, or empty for none.
  tier: string;
  score: number | null;
  passed: number;
  failed: number;
  skipped: number;
  errored: number;
  criteria: Record<string, { status: Status; message?: string }>;
};

export type EntityResult = Result & {
  blueprint: string;
  identifier: string;
  title: string;
  evaluatedAt: string;
};

export type Summary = {
  spec: ScorecardSpec;
  entities: number;
  evaluatedAt: string | null;
  // tiers counts entities by their highest tier; the first, named "", reached none.
  tiers: { name: string; count: number }[];
  criteria: {
    identifier: string;
    passed: number;
    failed: number;
    skipped: number;
    errored: number;
  }[];
};

export type ResultQuery = { tier?: string; q?: string; page?: number; size?: number };

export const scorecards = configDocs<ScorecardSpec>("scorecards");

const path = (identifier: string) => `scorecards/${encodeURIComponent(identifier)}`;

export const scorecardKeys = {
  summary: (identifier: string) => [...scorecards.keys.detail(identifier), "summary"] as const,
  results: (identifier: string, query: ResultQuery) =>
    [...scorecards.keys.detail(identifier), "results", query] as const,
  entity: (blueprint: string, identifier: string) =>
    ["entity-scorecards", blueprint, identifier] as const,
};

export async function getSummary(identifier: string): Promise<Summary> {
  return (await api.get(`${path(identifier)}/summary`).json<ApiDataResponse<Summary>>()).data;
}

export async function listResults(
  identifier: string,
  query: ResultQuery,
): Promise<{ results: EntityResult[]; total: number }> {
  const searchParams = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== "") searchParams.set(key, String(value));
  }
  return (
    await api
      .get(`${path(identifier)}/results`, { searchParams })
      .json<ApiDataResponse<{ results: EntityResult[]; total: number }>>()
  ).data;
}

export async function evaluateScorecard(identifier: string): Promise<{ entities: number }> {
  return (
    await api.post(`${path(identifier)}/evaluate`).json<ApiDataResponse<{ entities: number }>>()
  ).data;
}

export type EntityScorecard = EntityResult & { scorecard: string; scorecardTitle: string };

export async function listEntityScorecards(
  blueprint: string,
  identifier: string,
): Promise<EntityScorecard[]> {
  return (
    await api
      .get("entity-scorecards", { searchParams: { blueprint, identifier } })
      .json<ApiDataResponse<EntityScorecard[]>>()
  ).data;
}

export const summaryQueryOptions = (identifier: string) =>
  queryOptions({
    queryKey: scorecardKeys.summary(identifier),
    queryFn: () => getSummary(identifier),
  });

export const resultsQueryOptions = (identifier: string, query: ResultQuery) =>
  queryOptions({
    queryKey: scorecardKeys.results(identifier, query),
    queryFn: () => listResults(identifier, query),
  });

export const entityScorecardsQueryOptions = (blueprint: string, identifier: string) =>
  queryOptions({
    queryKey: scorecardKeys.entity(blueprint, identifier),
    queryFn: () => listEntityScorecards(blueprint, identifier),
  });
