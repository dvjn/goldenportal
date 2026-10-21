import { queryOptions, useMutation, useQueryClient, useSuspenseQuery } from "@tanstack/react-query";
import {
  catalogKeys,
  createEntity,
  deleteEntity,
  type EntityInput,
  type EntitySearch,
  getBlueprint,
  getBlueprints,
  getEntities,
  getEntity,
  updateEntity,
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

export function useSaveEntity(blueprint: string, existing: boolean) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (entity: EntityInput) =>
      existing ? updateEntity(blueprint, entity) : createEntity(blueprint, entity),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: catalogKeys.all }),
  });
}

export function useDeleteEntity(blueprint: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (identifier: string) => deleteEntity(blueprint, identifier),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: catalogKeys.all }),
  });
}
