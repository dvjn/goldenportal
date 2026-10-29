import { queryOptions, useMutation, useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { createServiceToken, getServiceTokens, revokeServiceToken, serviceTokenKeys } from "./api";

export const serviceTokensQueryOptions = queryOptions({
  queryKey: serviceTokenKeys.all,
  queryFn: getServiceTokens,
});

export function useServiceTokens() {
  return useSuspenseQuery(serviceTokensQueryOptions).data;
}

export function useCreateServiceToken() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: createServiceToken,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: serviceTokenKeys.all }),
  });
}

export function useRevokeServiceToken() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: revokeServiceToken,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: serviceTokenKeys.all }),
  });
}
