import { HTTPError } from "ky";
import { type ApiDataResponse, api } from "@/lib/api";

export type PropertyType = "string" | "number" | "boolean" | "array" | "object";

export type PropertyFormat = "url" | "email" | "date" | "date-time";

export type PropertyView = {
  key: string;
  title: string;
  description?: string;
  type: PropertyType;
  format?: PropertyFormat;
  enum?: string[];
};

export type RelationView = {
  key: string;
  title: string;
  description?: string;
  target: string;
  target_title: string;
  many: boolean;
};

export type InverseView = {
  source: string;
  source_title: string;
  relation: string;
  title: string;
};

export type Blueprint = {
  identifier: string;
  title: string;
  description?: string;
  icon?: string;
  entity_count: number;
  properties: PropertyView[];
  relations: RelationView[];
  inverses: InverseView[];
  columns: string[];
};

export type Integration = {
  identifier: string;
  title: string;
  manual: boolean;
};

export type EntitySummary = {
  identifier: string;
  title: string;
  integration: Integration;
  properties: Record<string, unknown>;
  updated_at: string;
};

export type EntityPage = {
  entities: EntitySummary[];
  offset: number;
  limit: number;
  total: number;
  integrations: (Integration & { count: number })[];
};

export type EntityName = {
  blueprint: string;
  identifier: string;
  title: string;
};

export type LinkGroup = {
  key: string;
  title: string;
  target: string;
  many: boolean;
  entities: EntityName[];
};

export type InverseGroup = InverseView & { entities: EntityName[] };

export type Entity = {
  blueprint: string;
  identifier: string;
  title: string;
  properties: Record<string, unknown>;
  integration: Integration;
  relations: LinkGroup[];
  incoming: InverseGroup[];
  created_at: string;
  updated_at: string;
};

export type EntityInput = {
  identifier: string;
  title: string;
  properties: Record<string, unknown>;
  relations: Record<string, string[]>;
};

export type EntitySearch = {
  q?: string;
  integration?: string;
  page: number;
};

export const entityPageSize = 25;

export const catalogKeys = {
  all: ["catalog"] as const,
  blueprints: ["catalog", "blueprints"] as const,
  blueprint: (blueprint: string) => ["catalog", "blueprints", blueprint] as const,
  entities: (blueprint: string, search: EntitySearch) =>
    ["catalog", "entities", blueprint, search] as const,
  entity: (blueprint: string, identifier: string) =>
    ["catalog", "entity", blueprint, identifier] as const,
};

const entitiesPath = (blueprint: string) =>
  `catalog/blueprints/${encodeURIComponent(blueprint)}/entities`;

const entityPath = (blueprint: string, identifier: string) =>
  `${entitiesPath(blueprint)}/${encodeURIComponent(identifier)}`;

export async function getBlueprints(): Promise<Blueprint[]> {
  return (await api.get("catalog/blueprints").json<ApiDataResponse<{ blueprints: Blueprint[] }>>())
    .data.blueprints;
}

export async function getBlueprint(blueprint: string): Promise<Blueprint> {
  return (
    await api
      .get(`catalog/blueprints/${encodeURIComponent(blueprint)}`)
      .json<ApiDataResponse<Blueprint>>()
  ).data;
}

export async function getEntities(blueprint: string, search: EntitySearch): Promise<EntityPage> {
  const searchParams = {
    q: search.q ?? "",
    integration: search.integration ?? "",
    offset: (search.page - 1) * entityPageSize,
    limit: entityPageSize,
  };
  return (
    await api.get(entitiesPath(blueprint), { searchParams }).json<ApiDataResponse<EntityPage>>()
  ).data;
}

export async function getEntity(blueprint: string, identifier: string): Promise<Entity> {
  return (await api.get(entityPath(blueprint, identifier)).json<ApiDataResponse<Entity>>()).data;
}

export async function createEntity(blueprint: string, entity: EntityInput): Promise<Entity> {
  return (await api.post(entitiesPath(blueprint), { json: entity }).json<ApiDataResponse<Entity>>())
    .data;
}

export async function updateEntity(blueprint: string, entity: EntityInput): Promise<Entity> {
  return (
    await api
      .put(entityPath(blueprint, entity.identifier), { json: entity })
      .json<ApiDataResponse<Entity>>()
  ).data;
}

export async function deleteEntity(blueprint: string, identifier: string) {
  await api.delete(entityPath(blueprint, identifier));
}

type FieldsErrorBody = { error?: { fields?: Record<string, string> } };

// fieldErrors returns the problems the API found with each field of an entity,
// by identifier, title, properties.<key> or relations.<key>.
export function fieldErrors(error: unknown): Record<string, string> {
  if (!(error instanceof HTTPError)) return {};
  return (error.data as FieldsErrorBody | undefined)?.error?.fields ?? {};
}
