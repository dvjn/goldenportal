import { HTTPError } from "ky";
import { type ApiDataResponse, api, sentenceCase } from "@/lib/api";

export type PropertyType = "string" | "number" | "boolean" | "array" | "object";

export type PropertyFormat = "url" | "email" | "date" | "date-time";

export type PropertyDisplay =
  | "text"
  | "mono"
  | "badge"
  | "status"
  | "relative"
  | "absolute"
  | "link"
  | "email"
  | "avatar"
  | "icon"
  | "number"
  | "list"
  | "json";

export type EnumColor = "gray" | "blue" | "green" | "yellow" | "orange" | "red" | "purple" | "teal";

// FilterKind picks the filter widget: enum and relation take a list of
// values, date and number take gte and lte bounds, boolean takes true or false.
export type FilterKind = "enum" | "boolean" | "number" | "date" | "relation";

export type PropertyView = {
  key: string;
  title: string;
  description?: string;
  type: PropertyType;
  format?: PropertyFormat;
  enum?: string[];
  display: PropertyDisplay;
  enum_colors?: Record<string, EnumColor>;
  enum_labels?: Record<string, string>;
  link_label?: string;
  empty_label?: string;
  hidden: boolean;
  sortable: boolean;
  filter_kind?: FilterKind;
};

export type RelationView = {
  key: string;
  title: string;
  description?: string;
  target: string;
  target_title: string;
  many: boolean;
  inverse?: string;
  inverse_title?: string;
};

// InverseView is a relation of another blueprint that links to this one. It
// has a key when the relation names its inverse; only then can columns,
// filters and derived paths use it.
export type InverseView = {
  key?: string;
  source: string;
  source_title: string;
  relation: string;
  title: string;
};

// DerivedView is a value read through relations. With property set, each
// value is that property of an entity the path reaches; without it, the
// value lists the entities of target that the path reaches.
export type DerivedView = {
  key: string;
  title: string;
  description?: string;
  path: string;
  target: string;
  target_title: string;
  many: boolean;
  empty_label?: string;
  sortable: boolean;
  property?: PropertyView;
};

export type DisplayGroup = {
  title: string;
  properties: string[];
};

export type BlueprintDisplay = {
  subtitle?: string;
  badges: string[];
  groups: DisplayGroup[];
};

export type SortDirection = "asc" | "desc";

// BlueprintTable has its defaults filled in. Columns and filters name
// properties, relations, inverse keys and derived keys; sort.by can also be
// "title" or "updatedAt".
export type BlueprintTable = {
  columns: string[];
  sort: { by: string; direction: SortDirection };
  filters: string[];
};

export type FedBy = {
  identifier: string;
  title: string;
  connector: string;
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
  derived: DerivedView[];
  display: BlueprintDisplay;
  table: BlueprintTable;
  /** The same as table.columns. */
  columns: string[];
  fed_by: FedBy[];
};

export type Integration = {
  identifier: string;
  title: string;
  manual: boolean;
};

export type EntityName = {
  blueprint: string;
  identifier: string;
  title: string;
};

// DerivedItem is one entity a derived path reaches, with the value of the
// property the path ends in, if it ends in one.
export type DerivedItem = {
  entity: EntityName;
  value?: unknown;
};

export type EntitySummary = {
  identifier: string;
  title: string;
  integration: Integration;
  properties: Record<string, unknown>;
  /** Targets by relation key and by inverse key; keys without links are absent. */
  relations: Record<string, EntityName[]>;
  /** Every derived key of the blueprint, with an empty list when nothing is reached. */
  derived: Record<string, DerivedItem[]>;
  updated_at: string;
};

export type EntityPage = {
  entities: EntitySummary[];
  offset: number;
  limit: number;
  total: number;
  sort: string;
  dir: SortDirection;
  integrations: (Integration & { count: number })[];
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
  derived: Record<string, DerivedItem[]>;
  created_at: string;
  updated_at: string;
};

export type EntityInput = {
  identifier: string;
  title: string;
  properties: Record<string, unknown>;
  relations: Record<string, string[]>;
};

export type Change = {
  blueprint: { identifier: string; title: string; icon?: string };
  identifier: string;
  title: string;
  integration: Integration;
  updated_at: string;
};

export type EntitySearch = {
  q?: string;
  integration?: string;
  page: number;
};

// EntityFilter is one filter of the entity list. in holds enum values,
// "true" or "false" for a boolean, or target identifiers for a relation,
// inverse or derived value that lists entities. gte and lte bound a date
// (YYYY-MM-DD or RFC 3339, lte takes in the whole day) or a number.
export type EntityFilter = {
  in?: string[];
  gte?: string;
  lte?: string;
  empty?: boolean;
};

export type EntityListQuery = {
  q?: string;
  integration?: string;
  /** 1-based. */
  page?: number;
  /** At most 100. */
  pageSize?: number;
  /** Defaults to the blueprint's table.sort. */
  sort?: string;
  dir?: SortDirection;
  filters?: Record<string, EntityFilter>;
};

export const entityPageSize = 25;

export const maxEntityPageSize = 100;

export const catalogKeys = {
  all: ["catalog"] as const,
  blueprints: ["catalog", "blueprints"] as const,
  blueprint: (blueprint: string) => ["catalog", "blueprints", blueprint] as const,
  entities: (blueprint: string, search: EntitySearch) =>
    ["catalog", "entities", blueprint, search] as const,
  entityList: (blueprint: string, query: EntityListQuery) =>
    ["catalog", "entities", blueprint, "list", query] as const,
  entity: (blueprint: string, identifier: string) =>
    ["catalog", "entity", blueprint, identifier] as const,
  changes: (limit: number) => ["catalog", "changes", limit] as const,
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

// entityListParams writes the query as the API reads it: filter[key]=a,b,
// filter[key][gte], filter[key][lte] and filter[key][empty].
export function entityListParams(query: EntityListQuery): URLSearchParams {
  const params = new URLSearchParams();
  const pageSize = Math.min(query.pageSize ?? entityPageSize, maxEntityPageSize);
  params.set("limit", String(pageSize));
  params.set("page", String(query.page ?? 1));
  if (query.q) params.set("q", query.q);
  if (query.integration) params.set("integration", query.integration);
  if (query.sort) params.set("sort", query.sort);
  if (query.dir) params.set("dir", query.dir);
  for (const [key, filter] of Object.entries(query.filters ?? {})) {
    const name = `filter[${key}]`;
    if (filter.in?.length) params.set(name, filter.in.join(","));
    if (filter.gte) params.set(`${name}[gte]`, filter.gte);
    if (filter.lte) params.set(`${name}[lte]`, filter.lte);
    if (filter.empty !== undefined) params.set(`${name}[empty]`, String(filter.empty));
  }
  return params;
}

export async function getEntityList(
  blueprint: string,
  query: EntityListQuery,
): Promise<EntityPage> {
  return (
    await api
      .get(entitiesPath(blueprint), { searchParams: entityListParams(query) })
      .json<ApiDataResponse<EntityPage>>()
  ).data;
}

export async function getEntities(blueprint: string, search: EntitySearch): Promise<EntityPage> {
  return getEntityList(blueprint, {
    q: search.q,
    integration: search.integration,
    page: search.page,
    pageSize: entityPageSize,
  });
}

export async function getEntity(blueprint: string, identifier: string): Promise<Entity> {
  return (await api.get(entityPath(blueprint, identifier)).json<ApiDataResponse<Entity>>()).data;
}

export async function getChanges(limit = 10): Promise<Change[]> {
  return (
    await api
      .get("catalog/changes", { searchParams: { limit } })
      .json<ApiDataResponse<{ changes: Change[] }>>()
  ).data.changes;
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

export function isNotFound(error: unknown): boolean {
  return error instanceof HTTPError && error.response.status === 404;
}

type FieldsErrorBody = { error?: { fields?: Record<string, string> } };

// fieldErrors returns the problems the API found with each field of an entity,
// by identifier, title, properties.<key> or relations.<key>.
export function fieldErrors(error: unknown): Record<string, string> {
  if (!(error instanceof HTTPError)) return {};
  const fields = (error.data as FieldsErrorBody | undefined)?.error?.fields ?? {};
  return Object.fromEntries(
    Object.entries(fields).map(([field, message]) => [field, sentenceCase(message)]),
  );
}
