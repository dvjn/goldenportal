import { type DateRange, isDuration } from "@/components/data-table";
import type { Filter } from "@/features/catalog";

type Value = string | number | boolean;

// CatalogFilters is the catalog table's filter in its URL: each property
// matches any of its values, and every property must match.
export type CatalogFilters = Record<string, Value[]>;

// CatalogLink is the catalog table's search for a link's entities: f and d,
// the values and days the table's own filters are set to, and where, the
// rest of the link's filter, which the table can only show or clear.
export type CatalogLink = { f?: CatalogFilters; d?: Record<string, DateRange>; where?: Filter };

const day = /^\d{4}-\d{2}-\d{2}$/;
const rangeOps = new Set(["gte", "gt", "lte", "lt"]);

function shiftDay(date: string, days: number) {
  const next = new Date(`${date}T00:00:00Z`);
  next.setUTCDate(next.getUTCDate() + days);
  return next.toISOString().slice(0, 10);
}

const isValue = (value: unknown): value is Value =>
  typeof value === "string" || typeof value === "number" || typeof value === "boolean";

// builtIns are the fields every entity has, of which the table only
// filters the day it was updated.
const builtIns = new Set([
  "identifier",
  "title",
  "blueprint",
  "integration",
  "createdAt",
  "updatedAt",
]);

// tableField reports whether the table filters a field: its own properties,
// scorecard tiers among them, and the day it was updated, not the other
// built-ins or anything a path reaches through a relation.
function tableField(property: string, date: boolean) {
  if (property.includes(".")) return false;
  if (!builtIns.has(property)) return true;
  return date && property === "updatedAt";
}

// matchesNothing reports an in with no values anywhere in a filter, which
// is how the server fills in a dashboard filter set to nothing. The empty
// list doesn't survive the trip back as JSON, so even under not, where it
// would match everything, it has no link.
function matchesNothing(filter: Filter): boolean {
  if ("and" in filter) return filter.and.some(matchesNothing);
  if ("or" in filter) return filter.or.some(matchesNothing);
  if ("not" in filter) return matchesNothing(filter.not);
  return filter.op === "in" && !filter.values?.length;
}

// toCatalogLink turns a link's filter, with its variables filled in by the
// server, into the catalog's search. Each condition the table's filters
// can say becomes one of them, so it can be changed there, and the rest
// stays as where. A filter that matches nothing has no link, since the
// catalog can't show nothing.
export function toCatalogLink(filter: Filter | undefined): CatalogLink | undefined {
  if (filter && matchesNothing(filter)) return undefined;
  const conditions = filter && "and" in filter ? filter.and : filter ? [filter] : [];
  const f: CatalogFilters = {};
  const d: Record<string, DateRange> = {};
  const rest: Filter[] = [];
  for (const condition of conditions) {
    if (!("property" in condition)) {
      rest.push(condition);
      continue;
    }
    const { property, op, value } = condition;
    if (op === "within" && isDuration(value) && tableField(property, true) && !d[property]) {
      d[property] = { within: value };
      continue;
    }
    if (rangeOps.has(op) && typeof value === "string" && day.test(value)) {
      const range = d[property] ?? {};
      const edge = op.startsWith("gt") ? "from" : "to";
      if (tableField(property, true) && !range[edge] && !range.within) {
        range[edge] = op === "gt" ? shiftDay(value, 1) : op === "lt" ? shiftDay(value, -1) : value;
        d[property] = range;
        continue;
      }
    }
    const values = op === "eq" ? [value] : op === "in" ? condition.values : undefined;
    if (values?.every(isValue) && tableField(property, false) && !f[property]) {
      f[property] = values;
      continue;
    }
    rest.push(condition);
  }
  const link: CatalogLink = {};
  if (Object.keys(f).length > 0) link.f = f;
  if (Object.keys(d).length > 0) link.d = d;
  if (rest.length > 0) link.where = rest.length === 1 ? rest[0] : { and: rest };
  return link;
}
