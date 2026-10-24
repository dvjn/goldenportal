import { queryOptions, useSuspenseQuery } from "@tanstack/react-query";
import { blueprints, listReferences, referencesKey } from "./api";

export const referencesQueryOptions = (identifier: string) =>
  queryOptions({ queryKey: referencesKey(identifier), queryFn: () => listReferences(identifier) });

export function useBlueprints() {
  return useSuspenseQuery(blueprints.queries.list).data;
}

export function useBlueprint(identifier: string) {
  return useSuspenseQuery(blueprints.queries.detail(identifier)).data;
}
