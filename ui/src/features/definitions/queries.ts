import { queryOptions, useMutation, useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { catalogKeys } from "@/features/catalog";
import { applyPlan, definitionKeys, getDefinitions, getSchema, planDefinitions } from "./api";

export const definitionsQueryOptions = queryOptions({
  queryKey: definitionKeys.current,
  queryFn: getDefinitions,
});

export const schemaQueryOptions = queryOptions({
  queryKey: definitionKeys.schema,
  queryFn: getSchema,
  staleTime: Number.POSITIVE_INFINITY,
});

export function useDefinitions() {
  return useSuspenseQuery(definitionsQueryOptions).data;
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
      ]),
  });
}
