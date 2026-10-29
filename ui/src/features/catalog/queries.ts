import {
  keepPreviousData,
  queryOptions,
  useMutation,
  useQuery,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { integrationsQueryOptions } from "@/features/integrations/queries";
import {
  catalogKeys,
  createEntity,
  deleteEntity,
  type EntityInput,
  type EntityListQuery,
  type EntitySearch,
  getBlueprint,
  getBlueprints,
  getChanges,
  getEntities,
  getEntity,
  getEntityList,
  maxEntityPageSize,
  updateEntity,
} from "./api";
import { sourceIcons } from "./sources";

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

export const entityListQueryOptions = (blueprint: string, query: EntityListQuery) =>
  queryOptions({
    queryKey: catalogKeys.entityList(blueprint, query),
    queryFn: () => getEntityList(blueprint, query),
    placeholderData: keepPreviousData,
  });

export const changesQueryOptions = (limit = 10) =>
  queryOptions({
    queryKey: catalogKeys.changes(limit),
    queryFn: () => getChanges(limit),
  });

export const entityQueryOptions = (blueprint: string, identifier: string) =>
  queryOptions({
    queryKey: catalogKeys.entity(blueprint, identifier),
    queryFn: () => getEntity(blueprint, identifier),
  });

export function useSourceIcons() {
  return useQuery({ ...integrationsQueryOptions, select: sourceIcons });
}

export function useEntityOptions(blueprint: string, q: string) {
  return useQuery({
    ...entityListQueryOptions(blueprint, { q: q || undefined, pageSize: maxEntityPageSize }),
    select: (page) => page.entities,
    staleTime: 60_000,
  });
}

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
