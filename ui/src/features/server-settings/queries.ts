import { queryOptions, useSuspenseQuery } from "@tanstack/react-query";
import { configurationKeys, getConfiguration } from "./api";

export const configurationQueryOptions = queryOptions({
  queryKey: configurationKeys.all,
  queryFn: getConfiguration,
  staleTime: Infinity,
});

export function useConfiguration() {
  return useSuspenseQuery(configurationQueryOptions).data;
}
