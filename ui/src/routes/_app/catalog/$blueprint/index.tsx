import { keepPreviousData, useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { Columns3, FileCode, LayoutGrid, RotateCcw, Rows3 } from "lucide-react";
import type { ReactNode } from "react";
import { RowActions, useRowActions } from "@/components/actions/entity-actions";
import { BlueprintIcon, IconTile } from "@/components/catalog/blueprint-icon";
import { EntityLink } from "@/components/catalog/entity-link";
import { EmptyValue, PropertyValue } from "@/components/catalog/property-value";
import { SplitTitle } from "@/components/catalog/split-title";
import { UnpublishedBlueprint } from "@/components/catalog/unpublished-blueprint";
import { valueLabel } from "@/components/catalog/value-label";
import {
  chipsOf,
  dateFilter,
  optionsFilter,
  replaceConditions,
  useFixedSummaries,
} from "@/components/catalog/where-filters";
import {
  DataTable,
  type DataTableChip,
  DataTableColumnHeader,
  type DataTableFilterField,
  type DataTableFilters,
  type DataTableFilterValue,
  type DataTableOption,
  dataTableColumns,
  pageSize,
  type TableSearch,
  usePageSize,
  validateTableSearch,
} from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { PageBreadcrumbs } from "@/components/page-breadcrumbs";
import { PageError } from "@/components/page-error";
import { PageHeader } from "@/components/page-header";
import { PageTab } from "@/components/page-tabs";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tabs, TabsContent, TabsList } from "@/components/ui/tabs";
import { WidgetGrid } from "@/components/widgets/widget-grid";
import { hasRole, useMe } from "@/features/auth";
import { type BlueprintSpec, blueprints, type Property } from "@/features/blueprints";
import {
  type Entity,
  type EntityQuery,
  entitiesQueryOptions,
  type Filter,
  facetsQueryOptions,
} from "@/features/catalog";
import { scorecards } from "@/features/scorecards";
import { formatAbsolute, formatRelative } from "@/lib/format";
import { usePageTitle } from "@/lib/page-title";

// CatalogSearch is the page's state in the URL: view is the open tab, a
// dashboard or "entities", and the rest is the table's; cols lists the
// shown columns, and filter the conditions every entity listed meets.
type CatalogSearch = TableSearch & {
  view?: string;
  filter?: Filter[];
  cols?: string;
};

const text = (value: unknown) => (typeof value === "string" && value !== "" ? value : undefined);

function validateCatalogSearch(search: Record<string, unknown>): CatalogSearch {
  return {
    ...validateTableSearch(search),
    view: text(search.view),
    filter: validFilter(search.filter),
    cols: text(search.cols),
  };
}

// isCondition checks a condition's shape; the server checks what it says.
function isCondition(value: unknown): value is Filter {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  const filter = value as Record<string, unknown>;
  if (Array.isArray(filter.and)) return filter.and.every(isCondition);
  if (Array.isArray(filter.or)) return filter.or.every(isCondition);
  if ("not" in filter) return isCondition(filter.not);
  return typeof filter.property === "string" && typeof filter.op === "string";
}

// validFilter keeps the conditions that have a shape. A URL written by hand
// may repeat one, which would be a chip each, so a repeat is left out.
function validFilter(filter: unknown): Filter[] | undefined {
  if (!Array.isArray(filter)) return undefined;
  const seen = new Set<string>();
  const conditions = filter.filter((condition) => {
    if (!isCondition(condition)) return false;
    const key = JSON.stringify(condition);
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
  return conditions.length > 0 ? conditions : undefined;
}

function queryFrom(blueprint: string, search: CatalogSearch, size: number): EntityQuery {
  return {
    blueprint,
    q: search.q,
    filter: search.filter && { and: search.filter },
    sort: search.sort,
    page: search.page,
    size,
  };
}

export const Route = createFileRoute("/_app/catalog/$blueprint/")({
  validateSearch: validateCatalogSearch,
  loaderDeps: ({ search: { view: _, ...table } }) => table,
  loader: ({ context, params, deps }) =>
    Promise.all([
      context.queryClient.ensureQueryData(blueprints.queries.detail(params.blueprint)),
      context.queryClient.ensureQueryData(
        entitiesQueryOptions(queryFrom(params.blueprint, deps, pageSize(deps))),
      ),
    ]),
  component: BlueprintCatalogPage,
  errorComponent: (props) => (
    <PageError {...props} back={<Link to="/catalog">Back to Catalog</Link>} />
  ),
});

// Lookups follow relations at read time, and objects and lists don't order,
// so the server can't sort by them. Scorecard tiers sort up their ladder.
function sortable(property: Property) {
  return (
    !property.lookup && !property.many && property.type !== "array" && property.type !== "object"
  );
}

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

// builtInColumns are the fields every entity has that can be columns,
// besides the title column, which shows the title and identifier.
const builtInColumns = [
  { id: "integration", title: "Integration" },
  { id: "createdAt", title: "Created" },
  { id: "updatedAt", title: "Updated" },
];

// defaultColumns are the blueprint's catalog columns, or else the first few
// properties that fit in a cell and when each entity was last updated.
function defaultColumns(spec: BlueprintSpec) {
  if (spec.catalog?.columns) return spec.catalog.columns;
  return [
    ...spec.properties
      .filter((property) => property.type !== "object")
      .slice(0, 5)
      .map((property) => property.identifier),
    "updatedAt",
  ];
}

const column = dataTableColumns<Entity>();

function BlueprintCatalogPage() {
  const { blueprint } = Route.useParams();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { data: doc } = useSuspenseQuery(blueprints.queries.detail(blueprint));
  const spec = doc.published?.spec;
  usePageTitle(spec?.title ?? doc.title);
  const builder = hasRole(useMe().role, "builder");
  const page = useQuery({
    ...entitiesQueryOptions(queryFrom(blueprint, search, usePageSize(search))),
    placeholderData: keepPreviousData,
  });
  const conditions = search.filter ?? [];
  const chipModels = chipsOf(conditions);
  const fixedSummaries = useFixedSummaries(
    blueprint,
    chipModels.flatMap((chip) => (chip.kind === "fixed" ? [chip.filter] : [])),
  );
  const rowActions = useRowActions(
    blueprint,
    (page.data?.items ?? []).map((entity) => entity.identifier),
  );

  if (!spec) return <UnpublishedBlueprint blueprint={blueprint} title={doc.title} />;

  const shown = search.cols?.split(",") ?? defaultColumns(spec);
  const setSearch = (patch: Partial<CatalogSearch>) =>
    navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true, resetScroll: false });

  // The subtitle the entity page shows also stands under each title.
  const subtitle = spec.properties.find(
    (property) => property.identifier === spec.entity?.header?.subtitle,
  );
  const columns = [
    column.accessor("title", {
      id: "title",
      header: ({ column }) => <DataTableColumnHeader column={column} title="Title" />,
      cell: ({ row }) => (
        <EntityLink
          blueprint={blueprint}
          identifier={row.original.identifier}
          className="grid max-w-72 hover:underline"
        >
          <span className="truncate font-medium" title={row.original.title}>
            <SplitTitle title={row.original.title} />
          </span>
          {subtitle ? (
            <span className="truncate text-xs text-muted-foreground">
              <PropertyValue
                property={subtitle}
                value={row.original.properties[subtitle.identifier]}
                links={row.original.related[subtitle.identifier]}
                compact
              />
            </span>
          ) : (
            row.original.title !== row.original.identifier && (
              <code
                className="truncate font-mono text-xs text-muted-foreground"
                title={row.original.identifier}
              >
                {row.original.identifier}
              </code>
            )
          )}
        </EntityLink>
      ),
    }),
    // Columns keep the order they're listed in.
    ...shown.flatMap((id) => {
      const builtIn = builtInColumns.find((c) => c.id === id);
      if (builtIn) {
        return [
          column.accessor((entity): unknown => entity[builtIn.id as "integration"], {
            id,
            header: ({ column }) => <DataTableColumnHeader column={column} title={builtIn.title} />,
            cell: ({ getValue }) => <BuiltInCell id={id} value={String(getValue() ?? "")} />,
          }),
        ];
      }
      const property = spec.properties.find((p) => p.identifier === id);
      if (!property) return [];
      return [
        column.accessor((entity) => entity.properties[property.identifier], {
          id: property.identifier,
          enableSorting: sortable(property),
          // A result is an object, which would sort descending first; tiers
          // start, like text, at the bottom of their ladder.
          sortDescFirst: property.scorecard ? false : undefined,
          header: ({ column }) => <DataTableColumnHeader column={column} title={property.title} />,
          cell: ({ row }) => (
            <PropertyValue
              property={property}
              value={row.original.properties[property.identifier]}
              links={row.original.related[property.identifier]}
              entity={row.original}
              compact
            />
          ),
        }),
      ];
    }),
    column.display({
      id: "actions",
      header: () => <span className="sr-only">Actions</span>,
      cell: ({ row }) => (
        <div className="text-right">
          <RowActions
            entity={{ blueprint, identifier: row.original.identifier, title: row.original.title }}
            actions={rowActions(row.original.identifier)}
          />
        </div>
      ),
    }),
  ];

  const setFilter = (next: Filter[]) =>
    setSearch({ filter: next.length > 0 ? next : undefined, page: undefined });
  // Every filter is offered, shown as a column or not. A date property whose
  // display asks for a filter gets the date one, not a list of its values.
  const filterFields: DataTableFilterField[] = [
    ...spec.properties
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
    ...spec.properties.filter(dateFilterable).map(
      (property): DataTableFilterField => ({
        kind: "date",
        id: property.identifier,
        title: property.title,
      }),
    ),
    { kind: "date", id: "updatedAt", title: "Updated" },
  ];
  // A link can filter by any property, so a chip whose property has no field
  // of its own gets one, to show and change it.
  const fieldFor = (kind: "options" | "date", id: string, values: string[]) => {
    const own = filterFields.find((field) => field.id === id && field.kind === kind);
    if (own) return own;
    const property = spec.properties.find((p) => p.identifier === id);
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
  const filterValue: DataTableFilters = {};
  const negated: Record<string, boolean> = {};
  let fixedChips = 0;
  for (const chip of chipModels) {
    const onRemove = () => setFilter(replaceConditions(conditions, chip.indices, []));
    if (chip.kind === "fixed") {
      chips.push({ ...fixedSummaries[fixedChips++], onRemove });
      continue;
    }
    const field = fieldFor(chip.kind, chip.property, chip.kind === "options" ? chip.values : []);
    if (chip.kind === "date") {
      filterValue[chip.property] = chip.range;
      chips.push({
        title: field.title,
        field,
        value: chip.range,
        onChange: (value) =>
          setFilter(
            replaceConditions(
              conditions,
              chip.indices,
              dateFilter(chip.property, Array.isArray(value) ? {} : value),
            ),
          ),
        onRemove,
      });
      continue;
    }
    // Property values are keyed by their JSON, so values of any type fit the picker.
    filterValue[chip.property] = chip.values;
    negated[chip.property] = chip.negated;
    chips.push({
      title: field.title,
      summary: chip.values.length === 0 ? (chip.negated ? "No Value" : "Any Value") : undefined,
      field,
      value: chip.values,
      negated: chip.negated,
      onChange: (value, not) =>
        setFilter(
          replaceConditions(
            conditions,
            chip.indices,
            optionsConditions(
              chip.property,
              Array.isArray(value) ? value : [],
              not ?? chip.negated,
              chip.values.length === 0,
            ),
          ),
        ),
      onRemove,
    });
  }
  // Picking a value for a property that has a chip changes it; otherwise
  // the condition joins the rest.
  const pick = (id: string, value: DataTableFilterValue | undefined, not?: boolean) => {
    const field = filterFields.find((f) => f.id === id);
    if (!field) return;
    const chip = chipModels.find(
      (c) => c.kind !== "fixed" && c.kind === field.kind && c.property === id,
    );
    const next =
      field.kind === "date"
        ? dateFilter(id, Array.isArray(value) ? {} : (value ?? {}))
        : optionsConditions(
            id,
            Array.isArray(value) ? value : [],
            not ?? false,
            chip?.kind === "options" && chip.values.length === 0,
          );
    setFilter(chip ? replaceConditions(conditions, chip.indices, next) : [...conditions, ...next]);
  };
  // The blueprint's own tabs come first, then the entities table every catalog page has.
  const views = spec.catalog?.tabs ?? [];
  const view = views.some((v) => v.identifier === search.view)
    ? search.view
    : search.view === "entities" || views.length === 0
      ? "entities"
      : views[0].identifier;

  const header = (tabs?: ReactNode) => (
    <PageHeader
      title={spec.title}
      icon={<IconTile icon={spec.icon} color={spec.color} size="lg" />}
      breadcrumbs={
        <PageBreadcrumbs page={spec.title}>
          <Link to="/catalog">Catalog</Link>
        </PageBreadcrumbs>
      }
      description={spec.description}
      actions={
        builder && (
          <Button variant="outline" asChild>
            <Link to="/blueprints/$identifier" params={{ identifier: blueprint }}>
              <FileCode />
              View Blueprint
            </Link>
          </Button>
        )
      }
      tabs={tabs}
    />
  );

  const table = (
    <DataTable
      noun="entities"
      data={page.data?.items ?? []}
      columns={columns}
      getRowId={(entity) => entity.identifier}
      search={search}
      searchLabel={`Search ${spec.title.toLowerCase()}`}
      rowCount={page.data?.total ?? 0}
      loading={page.isPlaceholderData}
      pending={page.isPending}
      error={page.error}
      onRetry={() => void page.refetch()}
      filters={{
        fields: filterFields,
        value: filterValue,
        negated,
        onChange: pick,
        chips,
      }}
      onClearFilters={() => setFilter([])}
      actions={
        // With no entities there are no columns to choose; a search or filter
        // that matches none keeps the menu, so the toolbar doesn't shift.
        (page.isPending || (page.data?.total ?? 0) > 0 || Boolean(search.q || search.filter)) && (
          <ColumnsMenu
            spec={spec}
            shown={shown}
            onChange={(cols) => setSearch({ cols: cols.join(",") })}
            onReset={search.cols === undefined ? undefined : () => setSearch({ cols: undefined })}
          />
        )
      }
      empty={
        <EmptyState
          icon={<LayoutGrid />}
          title={`No ${spec.title.toLowerCase()} entities yet`}
          description="They show up here once an integration writes them."
        />
      }
    />
  );

  if (views.length === 0) {
    return (
      <div className="flex flex-col gap-6">
        {header()}
        {table}
      </div>
    );
  }

  return (
    <Tabs
      value={view}
      onValueChange={(next) =>
        navigate({ search: (prev) => ({ ...prev, view: next }), replace: true })
      }
    >
      {header(
        <TabsList variant="line" aria-label={spec.title}>
          {views.map((v) => (
            <PageTab key={v.identifier} value={v.identifier}>
              {v.icon && <BlueprintIcon icon={v.icon} />}
              {v.title}
            </PageTab>
          ))}
          <PageTab value="entities">
            <Rows3 />
            Entities
            {page.data && (
              <span className="text-xs text-muted-foreground tabular-nums">{page.data.total}</span>
            )}
          </PageTab>
        </TabsList>,
      )}
      {views.map((v) => (
        <TabsContent key={v.identifier} value={v.identifier} className="pt-2">
          <WidgetGrid
            widgets={v.widgets}
            source={{ blueprint, page: "catalog", tab: v.identifier }}
            context={{}}
          />
        </TabsContent>
      ))}
      <TabsContent value="entities" className="pt-2">
        {table}
      </TabsContent>
    </Tabs>
  );
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

// BuiltInCell shows a field every entity has: dates as how long ago, and
// the integration that wrote it.
function BuiltInCell({ id, value }: Readonly<{ id: string; value: string }>) {
  if (!value) return <EmptyValue />;
  if (id === "integration") return <span className="text-muted-foreground">{value}</span>;
  return (
    <span className="whitespace-nowrap text-muted-foreground" title={formatAbsolute(value)}>
      {formatRelative(value)}
    </span>
  );
}

function ColumnsMenu({
  spec,
  shown,
  onChange,
  onReset,
}: Readonly<{
  spec: BlueprintSpec;
  shown: string[];
  onChange: (cols: string[]) => void;
  // onReset goes back to the blueprint's default columns, once others are chosen.
  onReset?: () => void;
}>) {
  const options = [
    ...spec.properties.map((p) => ({ id: p.identifier, title: p.title })),
    ...builtInColumns,
  ];
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline">
          <Columns3 />
          Columns
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-52">
        <DropdownMenuLabel>Show Columns</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {options.map((option) => (
          <DropdownMenuCheckboxItem
            key={option.id}
            checked={shown.includes(option.id)}
            onSelect={(event) => event.preventDefault()}
            onCheckedChange={(checked) =>
              onChange(
                options
                  .map((o) => o.id)
                  .filter((id) => (id === option.id ? checked : shown.includes(id))),
              )
            }
          >
            {option.title}
          </DropdownMenuCheckboxItem>
        ))}
        {onReset && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={onReset}>
              <RotateCcw />
              Reset to defaults
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
