import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { catalogKeys } from "@/features/catalog";
import { integrationKeys } from "@/features/integrations";
import {
  applyPlan,
  definitionKeys,
  getDefinitions,
  getDiff,
  getRevision,
  getRevisions,
  getSchema,
  planDefinitions,
  planRollback,
} from "./api";

export const definitionsQueryOptions = queryOptions({
  queryKey: definitionKeys.current,
  queryFn: getDefinitions,
});

export const schemaQueryOptions = queryOptions({
  queryKey: definitionKeys.schema,
  queryFn: getSchema,
  staleTime: Number.POSITIVE_INFINITY,
});

export const revisionsQueryOptions = (page: number) =>
  queryOptions({
    queryKey: definitionKeys.revisions(page),
    queryFn: () => getRevisions(page),
  });

export const revisionQueryOptions = (number: number) =>
  queryOptions({
    queryKey: definitionKeys.revision(number),
    queryFn: () => getRevision(number),
  });

export const diffQueryOptions = (from: number, to: number) =>
  queryOptions({
    queryKey: definitionKeys.diff(from, to),
    queryFn: () => getDiff(from, to),
  });

export function useRevisions(page: number) {
  return useQuery(revisionsQueryOptions(page));
}

export function useRevision(number: number) {
  return useQuery(revisionQueryOptions(number));
}

export function usePlanRollback() {
  return useMutation({ mutationFn: planRollback });
}

export function useDefinitions() {
  return useQuery(definitionsQueryOptions);
}

export function usePlanDefinitions() {
  return useMutation({ mutationFn: planDefinitions });
}

export function useApplyPlan() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ planId, message }: { planId: string; message: string }) =>
      applyPlan(planId, message),
    onSuccess: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: definitionKeys.all }),
        queryClient.invalidateQueries({ queryKey: catalogKeys.all }),
        queryClient.invalidateQueries({ queryKey: integrationKeys.all }),
      ]),
  });
}
