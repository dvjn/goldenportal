import { useQuery, useQueryClient } from "@tanstack/react-query";
import { type BlueprintSpec, blueprints, type Property } from "@/features/blueprints";
import { type Filter, facetsQueryOptions } from "@/features/catalog";
import { scorecards } from "@/features/scorecards";
import { formatAbsolute, formatDate } from "@/lib/format";

// WhereFilter is one condition of a link's where, said the way the table's
// own filter chips say theirs.
export type WhereFilter = { title: string; summary: string; filter: Filter };

// Named says what a property path is called, as the titles of the
// properties it passes through, and what its values are called when it
// ends at a relation.
type Named = { title: string; values?: Map<string, string> };

const builtins: Record<string, string> = {
  identifier: "Identifier",
  title: "Title",
  blueprint: "Blueprint",
  integration: "Integration",
  createdAt: "Created",
  updatedAt: "Updated",
};

// scoreFields name what a scorecard property's dot leads to in its result.
const scoreFields: Record<string, string> = {
  score: "Score",
  passed: "Passed",
  failed: "Failed",
  skipped: "Skipped",
  errored: "Errored",
  failing: "Failing",
  checks: "Checks",
};

// whereConditions lists where's conditions, each of which narrows the table.
export function whereConditions(where: Filter | undefined): Filter[] {
  if (!where) return [];
  return "and" in where ? where.and : [where];
}

// withoutCondition is where with its index'th condition removed.
export function withoutCondition(where: Filter, index: number): Filter | undefined {
  const rest = whereConditions(where).filter((_, i) => i !== index);
  return rest.length === 0 ? undefined : rest.length === 1 ? rest[0] : { and: rest };
}

function paths(filter: Filter): string[] {
  if ("and" in filter) return filter.and.flatMap(paths);
  if ("or" in filter) return filter.or.flatMap(paths);
  if ("not" in filter) return paths(filter.not);
  return [filter.property];
}

const day = /^\d{4}-\d{2}-\d{2}$/;
const instant = /^\d{4}-\d{2}-\d{2}T/;

function valueLabel(value: unknown, named?: Named) {
  if (typeof value === "boolean") return value ? "Yes" : "No";
  if (typeof value === "string") {
    if (day.test(value)) return formatDate(value);
    if (instant.test(value)) return formatAbsolute(value);
    return named?.values?.get(value) ?? value;
  }
  return String(value);
}

const compare: Record<string, string> = {
  gt: "over",
  gte: "at least",
  lt: "under",
  lte: "at most",
};
const compareDates: Record<string, string> = {
  gt: "after",
  gte: "from",
  lt: "before",
  lte: "until",
};

// describe says a condition as a chip's title and summary.
function describe(filter: Filter, names: Map<string, Named>): { title: string; summary: string } {
  if ("not" in filter) {
    const inner = describe(filter.not, names);
    return { title: inner.title, summary: `not ${inner.summary}` };
  }
  if ("and" in filter || "or" in filter) {
    const parts = "and" in filter ? filter.and : filter.or;
    const joiner = "and" in filter ? " and " : " or ";
    return {
      title: "and" in filter ? "All of" : "Any of",
      summary: parts
        .map((part) => {
          const { title, summary } = describe(part, names);
          return `${title}: ${summary}`;
        })
        .join(joiner),
    };
  }
  const named = names.get(filter.property);
  const title = named?.title ?? filter.property;
  const label = (value: unknown) => valueLabel(value, named);
  switch (filter.op) {
    case "eq":
      return { title, summary: label(filter.value) };
    case "in":
      return { title, summary: (filter.values ?? []).map(label).join(", ") };
    case "ne":
      return { title, summary: `not ${label(filter.value)}` };
    case "contains":
      return { title, summary: `contains ${label(filter.value)}` };
    case "exists":
      return { title, summary: "any value" };
    case "within":
      return { title, summary: `in the last ${label(filter.value)}` };
    default: {
      const date = typeof filter.value === "string" && day.test(filter.value.slice(0, 10));
      const word = (date ? compareDates : compare)[filter.op] ?? filter.op;
      return { title, summary: `${word} ${label(filter.value)}` };
    }
  }
}

// useWhereFilters says each of where's conditions with the titles of the
// properties it follows, such as "Service › Owner: Platform". Until they've
// loaded, conditions are said with their identifiers.
export function useWhereFilters(blueprint: string, where: Filter | undefined): WhereFilter[] {
  const client = useQueryClient();
  const conditions = whereConditions(where);
  const wanted = [...new Set(conditions.flatMap(paths))].sort();
  const { data: walked } = useQuery({
    queryKey: ["catalog", "where-names", blueprint, wanted],
    enabled: wanted.length > 0,
    staleTime: 60_000,
    queryFn: async () => {
      const named = new Map<string, Named>();
      // A blueprint that won't load leaves the rest of its path unnamed.
      for (const path of wanted) named.set(path, await name(path).catch(() => ({ title: path })));
      return named;
    },
  });

  // name walks a path through the blueprints its relations lead to.
  async function name(path: string): Promise<Named> {
    const titles: string[] = [];
    let current: string | undefined = blueprint;
    const segments = path.split(".");
    for (const [i, segment] of segments.entries()) {
      const last = i === segments.length - 1;
      if (segment in builtins && last) return { title: [...titles, builtins[segment]].join(" › ") };
      const spec: BlueprintSpec | undefined = current
        ? (await client.ensureQueryData(blueprints.queries.detail(current))).published?.spec
        : undefined;
      const property: Property | undefined = spec?.properties.find((p) => p.identifier === segment);
      if (!property || !current) return { title: [...titles, ...segments.slice(i)].join(" › ") };
      titles.push(property.title);
      if (property.scorecard) {
        if (!last) {
          const rest = segments.slice(i + 1).map((field) => scoreFields[field] ?? field);
          return { title: [...titles, ...rest].join(" › ") };
        }
        const doc = await client.ensureQueryData(scorecards.queries.detail(property.scorecard));
        const tiers = doc.published?.spec.tiers ?? [];
        return {
          title: titles.join(" › "),
          values: new Map([["", "No tier"], ...tiers.map((t) => [t.name, t.title] as const)]),
        };
      }
      if (last) {
        const title = titles.join(" › ");
        if (!property.relation) return { title };
        const facets = await client.ensureQueryData(facetsQueryOptions(current, segment));
        const values = new Map(
          facets.flatMap((f) =>
            typeof f.value === "string" && f.title ? [[f.value, f.title] as const] : [],
          ),
        );
        return { title, values };
      }
      current = property.relation;
    }
    return { title: titles.join(" › ") };
  }

  const names = new Map(walked);
  return conditions.map((filter) => ({ ...describe(filter, names), filter }));
}
