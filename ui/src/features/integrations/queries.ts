import { queryOptions, useMutation, useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import {
  createIngestToken,
  getIngestTokens,
  getIntegration,
  getIntegrations,
  getRuns,
  integrationKeys,
  type NewIngestToken,
  revokeIngestToken,
  syncIntegration,
} from "./api";

const runningPollMs = 2000;

export const integrationsQueryOptions = queryOptions({
  queryKey: integrationKeys.list,
  queryFn: getIntegrations,
});

export const integrationQueryOptions = (identifier: string) =>
  queryOptions({
    queryKey: integrationKeys.detail(identifier),
    queryFn: () => getIntegration(identifier),
    refetchInterval: (query) =>
      query.state.data?.last_run?.status === "running" ? runningPollMs : false,
  });

export const runsQueryOptions = (identifier: string) =>
  queryOptions({
    queryKey: integrationKeys.runs(identifier),
    queryFn: () => getRuns(identifier),
    refetchInterval: (query) =>
      query.state.data?.some((run) => run.status === "running") ? runningPollMs : false,
  });

export const ingestTokensQueryOptions = (identifier: string) =>
  queryOptions({
    queryKey: integrationKeys.tokens(identifier),
    queryFn: () => getIngestTokens(identifier),
  });

export function useIntegrations() {
  return useSuspenseQuery(integrationsQueryOptions).data;
}

export function useIntegration(identifier: string) {
  return useSuspenseQuery(integrationQueryOptions(identifier)).data;
}

export function useRuns(identifier: string) {
  return useSuspenseQuery(runsQueryOptions(identifier)).data;
}

export function useIngestTokens(identifier: string) {
  return useSuspenseQuery(ingestTokensQueryOptions(identifier)).data;
}

export function useSyncIntegration(identifier: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => syncIntegration(identifier),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: integrationKeys.all }),
  });
}

export function useCreateIngestToken(identifier: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (token: NewIngestToken) => createIngestToken(identifier, token),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: integrationKeys.tokens(identifier) }),
  });
}

export function useRevokeIngestToken(identifier: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => revokeIngestToken(identifier, id),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: integrationKeys.tokens(identifier) }),
  });
}
