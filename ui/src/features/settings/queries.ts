import { queryOptions, useMutation, useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import { getScim, getSettings, rotateScimToken, settingsKeys } from "./api";

export const settingsQueryOptions = queryOptions({
  queryKey: settingsKeys.settings,
  queryFn: getSettings,
});

export const scimQueryOptions = queryOptions({
  queryKey: settingsKeys.scim,
  queryFn: getScim,
});

export function useSettings() {
  return useSuspenseQuery(settingsQueryOptions).data;
}

export function useScim() {
  return useSuspenseQuery(scimQueryOptions).data;
}

export function useRotateScimToken() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: rotateScimToken,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: settingsKeys.scim }),
  });
}
