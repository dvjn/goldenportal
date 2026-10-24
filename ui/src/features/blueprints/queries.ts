import { queryOptions, useSuspenseQuery } from "@tanstack/react-query";
import { blueprints, getUsage, usageKey } from "./api";

export const usageQueryOptions = (identifier: string) =>
  queryOptions({ queryKey: usageKey(identifier), queryFn: () => getUsage(identifier) });

export function useBlueprints() {
  return useSuspenseQuery(blueprints.queries.list).data;
}

export function useBlueprint(identifier: string) {
  return useSuspenseQuery(blueprints.queries.detail(identifier)).data;
}
