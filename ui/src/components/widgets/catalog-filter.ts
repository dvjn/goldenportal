import type { Filter } from "@/features/catalog";

// CatalogLink is the catalog list's search for a link's entities: filter,
// the conditions the entities must all meet, as the server says them.
export type CatalogLink = { filter?: Filter[] };

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

// toCatalogLink turns a link's filter, with its variables filled in and
// simplified by the server, into the catalog's search: its conditions, as
// they stand. A filter that matches nothing has no link, since the catalog
// can't show nothing.
export function toCatalogLink(filter: Filter | undefined): CatalogLink | undefined {
  if (filter && matchesNothing(filter)) return undefined;
  const conditions = filter && "and" in filter ? filter.and : filter ? [filter] : [];
  return conditions.length > 0 ? { filter: conditions } : {};
}
