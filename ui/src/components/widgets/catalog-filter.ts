import type { Me } from "@/features/auth";
import type { Entity, Filter } from "@/features/catalog";

type Value = string | number | boolean;

// CatalogFilters is the catalog table's filter in its URL: each property
// matches any of its values, and every property must match.
export type CatalogFilters = Record<string, Value[]>;

// Context values a widget's filter can compare with. filters is what the
// dashboard's filters are set to, by identifier, as a WidgetContext has them.
export type FilterContext = {
  entity?: string;
  entityData?: Entity;
  me?: Me;
  filters?: Record<string, string[] | null>;
};

// meValues is what $me and $me.<attribute> stand for, as the server binds
// them, so a click shows the same entities the widget counted. An
// attribute with no value matches nothing.
function meValues(me: Me): Record<string, string[]> {
  return {
    $me: [me.email],
    "$me.email": [me.email],
    "$me.name": me.displayName ? [me.displayName] : [],
    "$me.role": [me.role],
    "$me.groups": me.groups ?? [],
  };
}

// toCatalogFilters turns a widget's filter into the catalog's, so a click
// shows the same entities. Filters the catalog can't express, such as ne
// or or, give undefined, and the widget offers no link rather than a wrong one.
export function toCatalogFilters(
  filter: Filter | undefined,
  context: FilterContext,
): CatalogFilters | undefined {
  if (!filter) return {};
  // bind gives a value's entities-worth of values: none for an unset
  // dashboard filter, which drops its condition as the server does. The
  // catalog can't show nothing, so a filter set to nothing has no link.
  const bind = (value: unknown): Value[] | undefined => {
    if (value === "$entity") return context.entity === undefined ? undefined : [context.entity];
    if (value === "$me" || (typeof value === "string" && value.startsWith("$me."))) {
      const values = context.me && meValues(context.me)[value];
      return values === undefined || values.length === 0 ? undefined : values;
    }
    if (typeof value === "string" && value.startsWith("$entity.")) {
      const entity = context.entityData;
      if (!entity) return undefined;
      const name = value.slice("$entity.".length);
      const raw =
        name === "identifier"
          ? entity.identifier
          : name === "title"
            ? entity.title
            : name === "blueprint"
              ? entity.blueprint
              : entity.properties[name];
      const items = Array.isArray(raw) ? raw : raw === null || raw === undefined ? [] : [raw];
      return items.length > 0 &&
        items.every(
          (item) =>
            typeof item === "string" || typeof item === "number" || typeof item === "boolean",
        )
        ? items
        : undefined;
    }
    if (typeof value === "string" && value.startsWith("$today")) {
      const match = /^\$today(?:([+-][0-9]+)d)?$/.exec(value);
      if (!match) return undefined;
      const days = Number(match[1] ?? 0);
      if (!Number.isSafeInteger(days) || Math.abs(days) > 365000) return undefined;
      const date = new Date();
      date.setUTCDate(date.getUTCDate() + days);
      return [date.toISOString().slice(0, 10)];
    }
    if (typeof value === "string" && value.startsWith("$filter.")) {
      const picked = context.filters?.[value.slice("$filter.".length)];
      return picked === null ? undefined : (picked ?? []);
    }
    return typeof value === "string" || typeof value === "number" || typeof value === "boolean"
      ? [value]
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
    const bound = raw?.map(bind);
    if (!bound || bound.some((values) => values === undefined)) return undefined;
    const values = (bound as Value[][]).flat();
    if (values.length === 0) continue;
    if (filters[condition.property]) return undefined;
    filters[condition.property] = values;
  }
  return filters;
}
