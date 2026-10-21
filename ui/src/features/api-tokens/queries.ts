import { queryOptions, useMutation, useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { apiTokenKeys, createApiToken, getApiTokens, revokeApiToken } from "./api";

export const apiTokensQueryOptions = queryOptions({
  queryKey: apiTokenKeys.all,
  queryFn: getApiTokens,
});

export function useApiTokens() {
  return useSuspenseQuery(apiTokensQueryOptions).data;
}

export function useCreateApiToken() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: createApiToken,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: apiTokenKeys.all }),
  });
}

export function useRevokeApiToken() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: revokeApiToken,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: apiTokenKeys.all }),
  });
}
