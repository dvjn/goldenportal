import { queryOptions, useSuspenseQuery } from "@tanstack/react-query";
import {
  connectionKeys,
  connections,
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
  queryKey: connectionKeys.connectors,
  queryFn: listConnectors,
  staleTime: Infinity,
});

export const tokensQueryOptions = (identifier: string) =>
  queryOptions({
    queryKey: connectionKeys.tokens(identifier),
    queryFn: () => listTokens(identifier),
  });

export const statusQueryOptions = queryOptions({
  queryKey: connectionKeys.status,
  queryFn: listStatus,
  refetchInterval: (query) =>
    pollWhileRunning((query.state.data ?? []).map((status) => status.lastRun)),
});

export const runsQueryOptions = (identifier: string, filters: RunFilters) =>
  queryOptions({
    queryKey: [...connectionKeys.runs(identifier), filters],
    queryFn: () => listRuns(identifier, filters),
    refetchInterval: (query) => pollWhileRunning(query.state.data?.items ?? []),
  });

export function useConnections() {
  return useSuspenseQuery(connections.queries.list).data;
}

export function useConnection(identifier: string) {
  return useSuspenseQuery(connections.queries.detail(identifier)).data;
}
