import { queryOptions, useSuspenseQuery } from "@tanstack/react-query";
import {
  catalogKeys,
  type EntitySearch,
  getBlueprint,
  getBlueprints,
  getEntities,
  getEntity,
} from "./api";

export const blueprintsQueryOptions = queryOptions({
  queryKey: catalogKeys.blueprints,
  queryFn: getBlueprints,
});

export const blueprintQueryOptions = (blueprint: string) =>
  queryOptions({
    queryKey: catalogKeys.blueprint(blueprint),
    queryFn: () => getBlueprint(blueprint),
  });

export const entitiesQueryOptions = (blueprint: string, search: EntitySearch) =>
  queryOptions({
    queryKey: catalogKeys.entities(blueprint, search),
    queryFn: () => getEntities(blueprint, search),
  });

export const entityQueryOptions = (blueprint: string, identifier: string) =>
  queryOptions({
    queryKey: catalogKeys.entity(blueprint, identifier),
    queryFn: () => getEntity(blueprint, identifier),
  });

export function useBlueprints() {
  return useSuspenseQuery(blueprintsQueryOptions).data;
}

export function useBlueprint(blueprint: string) {
  return useSuspenseQuery(blueprintQueryOptions(blueprint)).data;
}

export function useEntities(blueprint: string, search: EntitySearch) {
  return useSuspenseQuery(entitiesQueryOptions(blueprint, search)).data;
}

export function useEntity(blueprint: string, identifier: string) {
  return useSuspenseQuery(entityQueryOptions(blueprint, identifier)).data;
}
