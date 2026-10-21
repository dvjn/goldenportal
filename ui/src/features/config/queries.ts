import { useQuery } from "@tanstack/react-query";
import { configKeys, getConfig } from "./api";

export function useConfig() {
  return useQuery({
    queryKey: configKeys.current,
    queryFn: getConfig,
    staleTime: Infinity,
  });
}
