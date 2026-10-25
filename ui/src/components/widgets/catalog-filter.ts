import type { Filter } from "@/features/catalog";

type Value = string | number | boolean;

// CatalogFilters is the catalog table's filter in its URL: each property
// matches any of its values, and every property must match.
export type CatalogFilters = Record<string, Value[]>;

// Context values a widget's filter can compare with.
export type FilterContext = { entity?: string; me?: string };

// toCatalogFilters turns a widget's filter into the catalog's, so a click
// shows the same entities. Filters the catalog can't express, such as ne
// or or, give undefined, and the widget offers no link rather than a wrong one.
export function toCatalogFilters(
  filter: Filter | undefined,
  context: FilterContext,
): CatalogFilters | undefined {
  if (!filter) return {};
  const bind = (value: unknown): Value | undefined => {
    if (value === "$entity") return context.entity;
    if (value === "$me") return context.me;
    return typeof value === "string" || typeof value === "number" || typeof value === "boolean"
      ? value
      : undefined;
  };

  const conditions = "and" in filter ? filter.and : [filter];
  const filters: CatalogFilters = {};
  for (const condition of conditions) {
    if (!("property" in condition) || condition.property.startsWith("$")) return undefined;
    const raw =
      condition.op === "eq"
        ? [condition.value]
        : condition.op === "in"
          ? condition.values
          : undefined;
    const values = raw?.map(bind);
    if (!values || values.some((value) => value === undefined) || filters[condition.property]) {
      return undefined;
    }
    filters[condition.property] = values as Value[];
  }
  return filters;
}
