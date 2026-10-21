import { queryOptions, useSuspenseQuery } from "@tanstack/react-query";
import { configKeys, getConfig } from "./api";

export const configQueryOptions = queryOptions({
  queryKey: configKeys.current,
  queryFn: getConfig,
  staleTime: Infinity,
});

export function useConfig() {
  return useSuspenseQuery(configQueryOptions).data;
}
