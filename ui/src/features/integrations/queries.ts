import { queryOptions, useSuspenseQuery } from "@tanstack/react-query";
import {
  integrationKeys,
  integrations,
  listConnectors,
  listRuns,
  listStatus,
  listTokens,
  type RunFilters,
} from "./api";

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
});

export const runsQueryOptions = (identifier: string, filters: RunFilters) =>
  queryOptions({
    queryKey: [...integrationKeys.runs(identifier), filters],
    queryFn: () => listRuns(identifier, filters),
  });

export function useIntegrations() {
  return useSuspenseQuery(integrations.queries.list).data;
}

export function useIntegration(identifier: string) {
  return useSuspenseQuery(integrations.queries.detail(identifier)).data;
}
