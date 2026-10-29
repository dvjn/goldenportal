import type { EntityFilter, SortDirection } from "./api";

export type CatalogSearch = {
  q?: string;
  page?: number;
  pageSize?: number;
  sort?: string;
  dir?: SortDirection;
  columns?: string[];
  integration?: string;
  /** Every other key is a filter of the blueprint's table.filters. */
  [filter: string]: string | number | string[] | EntityFilter | undefined;
};

const reservedKeys = new Set(["q", "page", "pageSize", "sort", "dir", "columns", "integration"]);

function nonEmptyString(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

function positiveInteger(value: unknown): number | undefined {
  const number = typeof value === "string" ? Number(value) : value;
  return typeof number === "number" && Number.isInteger(number) && number > 0 ? number : undefined;
}

function stringList(value: unknown): string[] | undefined {
  if (typeof value === "string" && value !== "") return value.split(",");
  if (Array.isArray(value) && value.every((item) => typeof item === "string")) return value;
  return undefined;
}

function entityFilter(value: unknown): EntityFilter | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return undefined;
  const raw = value as Record<string, unknown>;
  const filter: EntityFilter = {
    in: stringList(raw.in),
    gte: nonEmptyString(raw.gte),
    lte: nonEmptyString(raw.lte),
    empty: typeof raw.empty === "boolean" ? raw.empty : undefined,
  };
  return isActive(filter) ? filter : undefined;
}

export function isActive(filter: EntityFilter | undefined): filter is EntityFilter {
  if (!filter) return false;
  return Boolean(filter.in?.length || filter.gte || filter.lte || filter.empty !== undefined);
}

export function parseCatalogSearch(raw: Record<string, unknown>): CatalogSearch {
  const search: CatalogSearch = {
    q: nonEmptyString(raw.q),
    page: positiveInteger(raw.page),
    pageSize: positiveInteger(raw.pageSize),
    sort: nonEmptyString(raw.sort),
    dir: raw.dir === "asc" || raw.dir === "desc" ? raw.dir : undefined,
    columns: stringList(raw.columns),
    integration: nonEmptyString(raw.integration),
  };
  for (const [key, value] of Object.entries(raw)) {
    if (reservedKeys.has(key)) continue;
    const filter = entityFilter(value);
    if (filter) search[key] = filter;
  }
  return search;
}

export function searchFilters(
  search: CatalogSearch,
  filterKeys: readonly string[],
): Record<string, EntityFilter> {
  const filters: Record<string, EntityFilter> = {};
  for (const key of filterKeys) {
    if (reservedKeys.has(key)) continue;
    const filter = search[key];
    if (typeof filter === "object" && !Array.isArray(filter) && isActive(filter)) {
      filters[key] = filter;
    }
  }
  return filters;
}

export function clearedFilters(filterKeys: readonly string[]): Partial<CatalogSearch> {
  return Object.fromEntries(filterKeys.map((key) => [key, undefined]));
}

export function toggleValue(values: string[], value: string): string[] {
  return values.includes(value)
    ? values.filter((candidate) => candidate !== value)
    : [...values, value];
}

export function listFilter(values: string[]): EntityFilter | undefined {
  return values.length === 0 ? undefined : { in: values };
}
