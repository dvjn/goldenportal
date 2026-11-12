import { useQueries, useQuery } from "@tanstack/react-query";
import { valueLabel } from "@/components/catalog/value-label";
import {
  chipsOf,
  dateFilter,
  optionsFilter,
  replaceConditions,
  useFixedSummaries,
} from "@/components/catalog/where-filters";
import type {
  DataTableChip,
  DataTableFilterField,
  DataTableFilters,
  DataTableFilterValue,
  DataTableOption,
} from "@/components/data-table";
import { checkStatuses } from "@/components/scorecards/check-status";
import type { BlueprintSpec, Property } from "@/features/blueprints";
import { type Filter, facetsQueryOptions } from "@/features/catalog";
import { type ScorecardSpec, type Status, scorecards } from "@/features/scorecards";

// CatalogFilters are a catalog page's filter as a table shows it: fields to
// pick from, and a chip per condition.
export type CatalogFilters = {
  filters: {
    fields: DataTableFilterField[];
    value: DataTableFilters;
    negated: Record<string, boolean>;
    onChange: (id: string, value: DataTableFilterValue | undefined, negated?: boolean) => void;
    chips: DataTableChip[];
  };
  onClearFilters: () => void;
  // set makes a field's chip match values, in place of any it had.
  set: (property: string, values: unknown[]) => void;
};

// filterable properties get a filter with their values to pick from: those
// whose display asks for one, or else those with few values.
function filterable(property: Property) {
  if (property.lookup) return false;
  if (property.display?.filter !== undefined) return property.display.filter;
  return (
    Boolean(property.relation) ||
    Boolean(property.scorecard) ||
    Boolean(property.enum) ||
    property.type === "boolean" ||
    (property.type === "array" && property.items?.type === "string")
  );
}

// dateFilterable properties get a date range filter unless their display says otherwise.
function dateFilterable(property: Property) {
  if (property.lookup || property.display?.filter === false) return false;
  return property.format === "date" || property.format === "date-time";
}

export const checkPath = (property: string, criterion: string) => `${property}.checks.${criterion}`;

const checkOptions: DataTableOption[] = (Object.keys(checkStatuses) as Status[]).map((status) => ({
  value: JSON.stringify(status),
  label: checkStatuses[status].label,
}));

// checkFields filter by each check of a scorecard property, named after
// the property, as a blueprint may have more than one scorecard.
function checkFields(property: Property, spec: ScorecardSpec | undefined): DataTableFilterField[] {
  return (spec?.tiers ?? []).flatMap((tier) =>
    tier.criteria.map(
      (criterion): DataTableFilterField => ({
        kind: "options",
        id: checkPath(property.identifier, criterion.identifier),
        title: `${property.title} › ${criterion.title}`,
        useOptions: () => checkOptions,
        negatable: true,
      }),
    ),
  );
}

// useCatalogFilters says conditions, the catalog's filter on a blueprint's
// entities, as a table's filters, and changes them with onChange.
export function useCatalogFilters(
  blueprint: string,
  spec: BlueprintSpec | undefined,
  conditions: Filter[],
  onChange: (next: Filter[]) => void,
): CatalogFilters {
  const properties = spec?.properties ?? [];
  const graded = properties.filter((property) => property.scorecard && filterable(property));
  const scorecardSpecs = useQueries({
    queries: graded.map((property) => scorecards.queries.detail(property.scorecard ?? "")),
    combine: (results) => results.map((result) => result.data?.published?.spec),
  });
  const chipModels = chipsOf(conditions);
  const fixedSummaries = useFixedSummaries(
    blueprint,
    chipModels.flatMap((chip) => (chip.kind === "fixed" ? [chip.filter] : [])),
  );

  // Every filter is offered, shown as a column or not. A date property whose
  // display asks for a filter gets the date one, not a list of its values.
  const fields: DataTableFilterField[] = [
    ...properties
      .filter((property) => filterable(property) && !dateFilterable(property))
      .map(
        (property): DataTableFilterField => ({
          kind: "options",
          id: property.identifier,
          title: property.title,
          useOptions: propertyOptions(blueprint, property),
          negatable: true,
        }),
      ),
    ...properties.filter(dateFilterable).map(
      (property): DataTableFilterField => ({
        kind: "date",
        id: property.identifier,
        title: property.title,
      }),
    ),
    { kind: "date", id: "updatedAt", title: "Updated" },
    ...graded.flatMap((property, i) => checkFields(property, scorecardSpecs[i])),
  ];
  // A link can filter by any property, so a chip whose property has no field
  // of its own gets one, to show and change it.
  const fieldFor = (kind: "options" | "date", id: string, values: string[]) => {
    const own = fields.find((field) => field.id === id && field.kind === kind);
    if (own) return own;
    const property = properties.find((p) => p.identifier === id);
    const title = property?.title ?? id;
    if (kind === "date") return { kind, id, title } satisfies DataTableFilterField;
    return {
      kind,
      id,
      title,
      negatable: true,
      useOptions: () =>
        values.map((key) => ({
          value: key,
          label: valueLabel(JSON.parse(key) as unknown, { property }),
        })),
    } satisfies DataTableFilterField;
  };

  const chips: DataTableChip[] = [];
  const value: DataTableFilters = {};
  const negated: Record<string, boolean> = {};
  let fixedChips = 0;
  for (const chip of chipModels) {
    const onRemove = () => onChange(replaceConditions(conditions, chip.indices, []));
    if (chip.kind === "fixed") {
      chips.push({ ...fixedSummaries[fixedChips++], onRemove });
      continue;
    }
    const field = fieldFor(chip.kind, chip.property, chip.kind === "options" ? chip.values : []);
    if (chip.kind === "date") {
      value[chip.property] = chip.range;
      chips.push({
        title: field.title,
        field,
        value: chip.range,
        onChange: (next) =>
          onChange(
            replaceConditions(
              conditions,
              chip.indices,
              dateFilter(chip.property, Array.isArray(next) ? {} : next),
            ),
          ),
        onRemove,
      });
      continue;
    }
    // Property values are keyed by their JSON, so values of any type fit the picker.
    value[chip.property] = chip.values;
    negated[chip.property] = chip.negated;
    chips.push({
      title: field.title,
      summary: chip.values.length === 0 ? (chip.negated ? "No Value" : "Any Value") : undefined,
      field,
      value: chip.values,
      negated: chip.negated,
      onChange: (next, not) =>
        onChange(
          replaceConditions(
            conditions,
            chip.indices,
            optionsConditions(
              chip.property,
              Array.isArray(next) ? next : [],
              not ?? chip.negated,
              chip.values.length === 0,
            ),
          ),
        ),
      onRemove,
    });
  }

  const optionsChip = (id: string) =>
    chipModels.find((c) => c.kind === "options" && c.property === id);
  // Picking a value for a property that has a chip changes it; otherwise
  // the condition joins the rest.
  const pick = (id: string, next: DataTableFilterValue | undefined, not?: boolean) => {
    const field = fields.find((f) => f.id === id);
    if (!field) return;
    const chip = chipModels.find(
      (c) => c.kind !== "fixed" && c.kind === field.kind && c.property === id,
    );
    const replacement =
      field.kind === "date"
        ? dateFilter(id, Array.isArray(next) ? {} : (next ?? {}))
        : optionsConditions(
            id,
            Array.isArray(next) ? next : [],
            not ?? false,
            chip?.kind === "options" && chip.values.length === 0,
          );
    onChange(
      chip
        ? replaceConditions(conditions, chip.indices, replacement)
        : [...conditions, ...replacement],
    );
  };

  return {
    filters: { fields, value, negated, onChange: pick, chips },
    onClearFilters: () => onChange([]),
    set: (property, values) => {
      const chip = optionsChip(property);
      const next = [
        optionsFilter(
          property,
          values.map((v) => JSON.stringify(v)),
          false,
        ),
      ];
      onChange(chip ? replaceConditions(conditions, chip.indices, next) : [...conditions, ...next]);
    },
  };
}

// optionsConditions says picked values as conditions. With none picked, a
// chip that only tested for a value stays that test, as toggling it between
// any value and no value does, and any other has nothing left to say.
function optionsConditions(
  property: string,
  keys: string[],
  negated: boolean,
  testsForValue: boolean,
): Filter[] {
  return keys.length === 0 && !testsForValue ? [] : [optionsFilter(property, keys, negated)];
}

// propertyOptions is the hook a property's filter loads its options with,
// once the filter is picked.
function propertyOptions(blueprint: string, property: Property) {
  return function useOptions() {
    return usePropertyOptions(blueprint, property);
  };
}

// usePropertyOptions loads the values a property can be filtered to. Enums
// and booleans offer their values, and scorecards no tier and their tiers;
// relations and tag lists offer what entities actually have. Options are
// keyed by their JSON, so values of any type fit the picker.
function usePropertyOptions(blueprint: string, property: Property): DataTableOption[] | undefined {
  const scorecard = useQuery({
    ...scorecards.queries.detail(property.scorecard ?? ""),
    enabled: Boolean(property.scorecard),
  });
  const tiers = scorecard.data?.published?.spec.tiers;
  const fixed = property.scorecard
    ? tiers && ["", ...tiers.map((tier) => tier.name)]
    : (property.enum ?? (property.type === "boolean" ? [true, false] : undefined));
  const facets = useQuery({
    ...facetsQueryOptions(blueprint, property.identifier),
    enabled: !property.scorecard && (!fixed || property.type === "boolean"),
  });
  if (!fixed && !facets.data) return undefined;
  const counts = new Map(facets.data?.map((facet) => [JSON.stringify(facet.value), facet]));
  const values = fixed ?? facets.data?.map((facet) => facet.value) ?? [];

  const titles = new Map<string, string>(
    property.scorecard
      ? [["", "No Tier"], ...(tiers ?? []).map((tier) => [tier.name, tier.title] as const)]
      : (facets.data ?? []).flatMap((facet) =>
          typeof facet.value === "string" && facet.title
            ? [[facet.value, facet.title] as const]
            : [],
        ),
  );

  return values.map((value) => {
    const key = JSON.stringify(value);
    const facet = counts.get(key);
    return {
      value: key,
      label: valueLabel(value, { property, titles }),
      count: facet?.count ?? (facets.data ? 0 : undefined),
    };
  });
}
