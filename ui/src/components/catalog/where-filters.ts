import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  type DateRange,
  dateRangeBounds,
  describeDateRange,
  isDuration,
  isoDate,
} from "@/components/data-table";
import { type BlueprintSpec, blueprints, type Property } from "@/features/blueprints";
import { type Filter, facetsQueryOptions } from "@/features/catalog";
import { scorecards } from "@/features/scorecards";
import { type ValueNames, valueLabel } from "./value-label";

// FilterChip is one top-level condition of the catalog's filter, as the
// filter box shows it. indices are the conditions it says: a day range says
// up to two, one for each end.
export type FilterChip = { indices: number[] } & (
  | {
      // An options chip is a property matching any of its values, or, with
      // none, having a value at all. Its values are JSON, as the picker's are.
      kind: "options";
      property: string;
      values: string[];
      negated: boolean;
    }
  | { kind: "date"; property: string; range: DateRange }
  // A fixed chip is any condition the menu can't say, to show and remove.
  | { kind: "fixed"; filter: Filter }
);

type Value = string | number | boolean;

const isValue = (value: unknown): value is Value =>
  typeof value === "string" || typeof value === "number" || typeof value === "boolean";

// builtIns are the fields every entity has, of which the menu only filters
// the day it was updated.
const builtIns = new Set([
  "identifier",
  "title",
  "blueprint",
  "integration",
  "createdAt",
  "updatedAt",
]);

// menuField reports whether the menu filters a field: the blueprint's own
// properties, scorecard tiers among them, and the day it was updated, not
// the other built-ins or anything a path reaches through a relation.
function menuField(property: string, date: boolean) {
  if (property.includes(".")) return false;
  if (!builtIns.has(property)) return true;
  return date && property === "updatedAt";
}

const day = /^\d{4}-\d{2}-\d{2}$/;
const instant = /^\d{4}-\d{2}-\d{2}T/;
const rangeOps = new Set(["gte", "gt", "lte", "lt"]);

function shiftDay(date: string, days: number) {
  const next = new Date(`${date}T00:00:00Z`);
  next.setUTCDate(next.getUTCDate() + days);
  return next.toISOString().slice(0, 10);
}

// dayBound is the end of a range of days a comparison with a day or an
// instant says, if the menu can say it: an instant has to be where a day
// starts, as the menu's own are.
function dayBound(op: string, value: unknown): { edge: "from" | "to"; day: string } | undefined {
  if (typeof value !== "string") return undefined;
  const from = op.startsWith("gt");
  if (day.test(value)) {
    const shift = op === "gt" ? 1 : op === "lt" ? -1 : 0;
    return { edge: from ? "from" : "to", day: shiftDay(value, shift) };
  }
  if (!instant.test(value) || (op !== "gte" && op !== "lt")) return undefined;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return undefined;
  if (date.getHours() + date.getMinutes() + date.getSeconds() + date.getMilliseconds() !== 0) {
    return undefined;
  }
  if (from) return { edge: "from", day: isoDate(date) };
  date.setDate(date.getDate() - 1);
  return { edge: "to", day: isoDate(date) };
}

type Leaf = Extract<Filter, { property: string }>;

// optionsOf reads a condition the options menu says: a property equal to,
// or in, some values, or not so, or with a value or without.
function optionsOf(
  filter: Filter,
): { property: string; values: Value[]; negated: boolean } | undefined {
  const negated = "not" in filter;
  const leaf = "not" in filter ? filter.not : filter;
  if (!("property" in leaf) || !menuField(leaf.property, false)) return undefined;
  const { property, op, value } = leaf;
  if (op === "exists") return { property, values: [], negated };
  if (op === "ne" && negated) return undefined;
  if (op === "eq" || op === "ne") {
    return isValue(value)
      ? { property, values: [value], negated: negated !== (op === "ne") }
      : undefined;
  }
  if (op === "in" && leaf.values?.length && leaf.values.every(isValue)) {
    return { property, values: leaf.values, negated };
  }
  return undefined;
}

// dateOf reads a condition the date menu says.
function dateOf(
  filter: Filter,
): { property: string; range: DateRange; bound?: "from" | "to" } | undefined {
  if (!("property" in filter) || !menuField(filter.property, true)) return undefined;
  const { property, op, value } = filter;
  if (op === "within")
    return isDuration(value) ? { property, range: { within: value } } : undefined;
  if (!rangeOps.has(op)) return undefined;
  const bound = dayBound(op, value);
  return bound && { property, range: { [bound.edge]: bound.day }, bound: bound.edge };
}

// chipsOf says each condition of a filter as a chip, in order. A property
// has one chip the menu can edit; a second condition on it is fixed.
export function chipsOf(conditions: Filter[]): FilterChip[] {
  const chips: FilterChip[] = [];
  const edited = new Map<string, FilterChip>();
  conditions.forEach((filter, index) => {
    const options = optionsOf(filter);
    const date = options ? undefined : dateOf(filter);
    const taken = edited.get(options?.property ?? date?.property ?? "");
    if (options && !taken) {
      const chip: FilterChip = {
        kind: "options",
        indices: [index],
        property: options.property,
        values: options.values.map((v) => JSON.stringify(v)),
        negated: options.negated,
      };
      edited.set(options.property, chip);
      chips.push(chip);
      return;
    }
    if (date && !taken) {
      const chip: FilterChip = {
        kind: "date",
        indices: [index],
        property: date.property,
        range: date.range,
      };
      edited.set(date.property, chip);
      chips.push(chip);
      return;
    }
    // A day range takes a second condition, for its other end.
    if (date?.bound && taken?.kind === "date" && !taken.range.within && !taken.range[date.bound]) {
      taken.range[date.bound] = date.range[date.bound];
      taken.indices.push(index);
      return;
    }
    chips.push({ kind: "fixed", indices: [index], filter });
  });
  return chips;
}

// replaceConditions swaps the conditions a chip says for others, where the
// first of them stood.
export function replaceConditions(conditions: Filter[], indices: number[], next: Filter[]) {
  return conditions.flatMap((filter, i) => {
    if (i === indices[0]) return next;
    return indices.includes(i) ? [] : [filter];
  });
}

// optionsFilter says picked values as conditions: one value is eq or ne,
// several are in or not in, and none is having a value or not.
export function optionsFilter(property: string, keys: string[], negated: boolean): Filter {
  const values = keys.map((key) => JSON.parse(key) as unknown);
  let filter: Filter;
  if (values.length === 0) filter = { property, op: "exists" };
  else if (values.length === 1) {
    return { property, op: negated ? "ne" : "eq", value: values[0] };
  } else filter = { property, op: "in", values };
  return negated ? { not: filter } : filter;
}

// dateFilter says a range of days as conditions, the way the date menu
// does: a within, or instants where the days start.
export function dateFilter(property: string, range: DateRange): Filter[] {
  if (range.within) return [{ property, op: "within", value: range.within }];
  const { from, before } = dateRangeBounds(range);
  return [
    ...(from ? [{ property, op: "gte", value: from }] : []),
    ...(before ? [{ property, op: "lt", value: before }] : []),
  ];
}

// Named says what a property path is called, as the titles of the
// properties it passes through, and what its values are called when it
// ends at a relation or a scorecard.
type Named = { title: string; values?: Map<string, string>; property?: Property };

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

function paths(filter: Filter): string[] {
  if ("and" in filter) return filter.and.flatMap(paths);
  if ("or" in filter) return filter.or.flatMap(paths);
  if ("not" in filter) return paths(filter.not);
  return [filter.property];
}

const compare: Record<string, string> = {
  gt: "Over",
  gte: "At Least",
  lt: "Under",
  lte: "At Most",
};
const compareDates: Record<string, string> = {
  gt: "After",
  gte: "From",
  lt: "Before",
  lte: "Until",
};

// leafSummary says a comparison on one field, as the filter menu's chips do.
function leafSummary(leaf: Leaf, negated: boolean, names: ValueNames): string {
  const label = (value: unknown) => valueLabel(value, names);
  switch (leaf.op) {
    case "eq":
    case "in": {
      const labels = (leaf.op === "eq" ? [leaf.value] : (leaf.values ?? [])).map(label).join(", ");
      return negated ? `Not ${labels}` : labels;
    }
    case "ne":
      return negated ? label(leaf.value) : `Not ${label(leaf.value)}`;
    case "exists":
      return negated ? "No Value" : "Any Value";
    case "contains":
      return `${negated ? "Lacks" : "Has"} ${label(leaf.value)}`;
    case "within":
      return `${negated ? "Not " : ""}${describeDateRange({ within: String(leaf.value) })}`;
    default: {
      const dated = typeof leaf.value === "string" && day.test(leaf.value.slice(0, 10));
      const word = (dated ? compareDates : compare)[leaf.op] ?? leaf.op;
      return `${negated ? "Not " : ""}${word} ${label(leaf.value)}`;
    }
  }
}

// describe says a condition as a chip's title and summary.
function describe(
  filter: Filter,
  named: (property: string) => Named | undefined,
): { title: string; summary: string } {
  if ("and" in filter || "or" in filter) {
    const all = "and" in filter;
    const parts = (all ? filter.and : filter.or).map((part) => {
      const { title, summary } = describe(part, named);
      return `${title}: ${summary}`;
    });
    return { title: all ? "All Of" : "Any Of", summary: parts.join(all ? " and " : " or ") };
  }
  const negated = "not" in filter;
  const inner = "not" in filter ? filter.not : filter;
  if (!("property" in inner)) {
    const { title, summary } = describe(inner, named);
    return { title, summary: `Not ${summary}` };
  }
  const known = named(inner.property);
  return {
    title: known?.title ?? inner.property,
    summary: leafSummary(inner, negated, { property: known?.property, titles: known?.values }),
  };
}

// FixedSummary is what a fixed chip reads as.
export type FixedSummary = { title: string; summary: string };

// useFixedSummaries says each fixed chip's condition with the titles of the
// properties it follows, such as "Service › Owner: Platform". Until they've
// loaded, conditions are said with their identifiers.
export function useFixedSummaries(blueprint: string, filters: Filter[]): FixedSummary[] {
  const client = useQueryClient();
  const wanted = [...new Set(filters.flatMap(paths))].sort();
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
          values: new Map([["", "No Tier"], ...tiers.map((t) => [t.name, t.title] as const)]),
        };
      }
      if (last) {
        const title = titles.join(" › ");
        if (!property.relation) return { title, property };
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

  return filters.map((filter) => describe(filter, (property) => walked?.get(property)));
}
