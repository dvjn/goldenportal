import { queryOptions } from "@tanstack/react-query";
import { type ApiDataResponse, api } from "@/lib/api";

export type CatalogEntry = {
  identifier: string;
  title: string;
  description?: string;
  icon?: string;
  entities: number;
};

// Link is a related entity. title is missing when it isn't ingested yet.
export type Link = { identifier: string; title?: string };

export type Entity = {
  blueprint: string;
  identifier: string;
  title: string;
  // properties include relations, computed values and resolved lookups.
  properties: Record<string, unknown>;
  scorecards?: Record<string, { tier: string; score: number | null }>;
  related: Record<string, Link[]>;
  integration: string;
  createdAt: string;
  updatedAt: string;
};

export type Incoming = {
  blueprint: string;
  blueprintTitle: string;
  property: string;
  propertyTitle: string;
  entities: Link[];
  total: number;
};

export type EntityDetail = Entity & { incoming: Incoming[] };

export type EntityPage = { entities: Entity[]; total: number };

// Filter is the server's filter tree: and, or and not over property conditions.
export type Filter =
  | { and: Filter[] }
  | { or: Filter[] }
  | { not: Filter }
  | { property: string; op: string; value?: unknown; values?: unknown[] };

export type EntityQuery = {
  blueprint: string;
  q?: string;
  filter?: Filter;
  sort?: string;
  page?: number;
  size?: number;
};

export type Facet = { value: unknown; title?: string; count: number };

export const catalogKeys = {
  index: ["catalog"] as const,
  entities: (query: EntityQuery) => ["catalog", "entities", query] as const,
  entity: (blueprint: string, identifier: string) =>
    ["catalog", "entity", blueprint, identifier] as const,
  facets: (blueprint: string, property: string) =>
    ["catalog", "facets", blueprint, property] as const,
};

export async function listCatalog(): Promise<CatalogEntry[]> {
  return (await api.get("catalog").json<ApiDataResponse<CatalogEntry[]>>()).data;
}

export async function listEntities(query: EntityQuery): Promise<EntityPage> {
  const searchParams = new URLSearchParams({ blueprint: query.blueprint });
  if (query.q) searchParams.set("q", query.q);
  if (query.filter) searchParams.set("filter", JSON.stringify(query.filter));
  if (query.sort) searchParams.set("sort", query.sort);
  if (query.page) searchParams.set("page", String(query.page));
  if (query.size) searchParams.set("size", String(query.size));
  return (await api.get("entities", { searchParams }).json<ApiDataResponse<EntityPage>>()).data;
}

export async function getEntity(blueprint: string, identifier: string): Promise<EntityDetail> {
  // Identifiers may hold slashes, which the server reads as part of the path.
  const path = identifier.split("/").map(encodeURIComponent).join("/");
  return (
    await api
      .get(`entities/${encodeURIComponent(blueprint)}/${path}`)
      .json<ApiDataResponse<EntityDetail>>()
  ).data;
}

export async function listFacets(blueprint: string, property: string): Promise<Facet[]> {
  return (
    await api
      .get("entities/facets", { searchParams: { blueprint, property } })
      .json<ApiDataResponse<Facet[]>>()
  ).data;
}

export const catalogQueryOptions = queryOptions({
  queryKey: catalogKeys.index,
  queryFn: listCatalog,
});

export const entitiesQueryOptions = (query: EntityQuery) =>
  queryOptions({ queryKey: catalogKeys.entities(query), queryFn: () => listEntities(query) });

export const entityQueryOptions = (blueprint: string, identifier: string) =>
  queryOptions({
    queryKey: catalogKeys.entity(blueprint, identifier),
    queryFn: () => getEntity(blueprint, identifier),
  });

export const facetsQueryOptions = (blueprint: string, property: string) =>
  queryOptions({
    queryKey: catalogKeys.facets(blueprint, property),
    queryFn: () => listFacets(blueprint, property),
  });
