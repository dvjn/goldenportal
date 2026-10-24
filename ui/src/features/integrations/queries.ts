import { queryOptions, useSuspenseQuery } from "@tanstack/react-query";
import { integrationKeys, integrations, listConnectors, listRuns, listTokens } from "./api";

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

export const runsQueryOptions = (identifier: string) =>
  queryOptions({ queryKey: integrationKeys.runs(identifier), queryFn: () => listRuns(identifier) });

export function useIntegrations() {
  return useSuspenseQuery(integrations.queries.list).data;
}

export function useIntegration(identifier: string) {
  return useSuspenseQuery(integrations.queries.detail(identifier)).data;
}
