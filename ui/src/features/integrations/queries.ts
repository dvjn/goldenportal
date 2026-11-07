import { queryOptions, useSuspenseQuery } from "@tanstack/react-query";
import {
  integrationKeys,
  integrations,
  listConnectors,
  listRuns,
  listStatus,
  listTokens,
  type Run,
  type RunFilters,
} from "./api";

// While a sync runs, what shows it is read again a few seconds apart, as action runs are.
const runningPoll = 2000;
const pollWhileRunning = (runs: (Run | null)[]) =>
  runs.some((run) => run && !run.finishedAt) ? runningPoll : false;

export const connectorsQueryOptions = queryOptions({
  queryKey: integrationKeys.connectors,
  queryFn: listConnectors,
  staleTime: Infinity,
});

export const tokensQueryOptions = (identifier: string) =>
  queryOptions({
    queryKey: integrationKeys.tokens(identifier),
    queryFn: () => listTokens(identifier),
  });

export const statusQueryOptions = queryOptions({
  queryKey: integrationKeys.status,
  queryFn: listStatus,
  refetchInterval: (query) =>
    pollWhileRunning((query.state.data ?? []).map((status) => status.lastRun)),
});

export const runsQueryOptions = (identifier: string, filters: RunFilters) =>
  queryOptions({
    queryKey: [...integrationKeys.runs(identifier), filters],
    queryFn: () => listRuns(identifier, filters),
    refetchInterval: (query) => pollWhileRunning(query.state.data?.items ?? []),
  });

export function useIntegrations() {
  return useSuspenseQuery(integrations.queries.list).data;
}

export function useIntegration(identifier: string) {
  return useSuspenseQuery(integrations.queries.detail(identifier)).data;
}
