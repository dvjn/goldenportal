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
import { useWhereFilters, withoutCondition } from "@/components/catalog/where-filters";
import {
  DataTable,
  DataTableColumnHeader,
  type DataTableFilterField,
  type DataTableFilters,
  type DataTableOption,
  type DateRange,
  dataTableColumns,
  dateRangeBounds,
  isDuration,
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

// Filters maps a property to the values it's filtered to.
type Filters = Record<string, (string | number | boolean)[]>;

// CatalogSearch is the page's state in the URL: view is the open tab, a
// dashboard or "entities", and the rest is the table's; cols lists the
// shown columns.
type CatalogSearch = TableSearch & {
  view?: string;
  f?: Filters;
  // d maps a date property, or updatedAt, to the range of days it's filtered to.
  d?: Record<string, DateRange>;
  // where is a filter a widget's link brought along that f and d can't say.
  where?: Filter;
  cols?: string;
};

const text = (value: unknown) => (typeof value === "string" && value !== "" ? value : undefined);

function validateCatalogSearch(search: Record<string, unknown>): CatalogSearch {
  return {
    ...validateTableSearch(search),
    view: text(search.view),
    f: validFilters(search.f),
    d: validDates(search.d),
    where: validWhere(search.where),
    cols: text(search.cols),
  };
}

function validFilters(f: unknown): Filters | undefined {
  if (typeof f !== "object" || f === null || Array.isArray(f)) return undefined;
  const filters: Filters = {};
  for (const [key, values] of Object.entries(f)) {
    if (
      Array.isArray(values) &&
      values.length > 0 &&
      values.every((value) => ["string", "number", "boolean"].includes(typeof value))
    ) {
      filters[key] = values;
    }
  }
  return Object.keys(filters).length > 0 ? filters : undefined;
}

// validWhere keeps a filter's shape; the server checks what it says.
function validWhere(where: unknown): Filter | undefined {
  return typeof where === "object" && where !== null && !Array.isArray(where)
    ? (where as Filter)
    : undefined;
}

const day = /^\d{4}-\d{2}-\d{2}$/;

function validDates(d: unknown): Record<string, DateRange> | undefined {
  if (typeof d !== "object" || d === null || Array.isArray(d)) return undefined;
  const dates: Record<string, DateRange> = {};
  for (const [key, range] of Object.entries(d)) {
    if (typeof range !== "object" || range === null) continue;
    const { from, to, within } = range as Record<string, unknown>;
    if (isDuration(within)) {
      dates[key] = { within };
      continue;
    }
    const valid: DateRange = {
      from: typeof from === "string" && day.test(from) ? from : undefined,
      to: typeof to === "string" && day.test(to) ? to : undefined,
    };
    if (valid.from || valid.to) dates[key] = valid;
  }
  return Object.keys(dates).length > 0 ? dates : undefined;
}

// toFilter turns the picked values into the server's filter: each property
// matches any of its values, dates fall in their ranges, and every property
// must match, as must where.
function toFilter(
  filters: Filters | undefined,
  dates: Record<string, DateRange> | undefined,
  where?: Filter,
): Filter | undefined {
  const conditions: Filter[] = Object.entries(filters ?? {}).map(([property, values]) => ({
    property,
    op: "in",
    values,
  }));
  for (const [property, range] of Object.entries(dates ?? {})) {
    // The server reaches back from its own now, as dashboards do.
    if (range.within) {
      conditions.push({ property, op: "within", value: range.within });
      continue;
    }
    const { from, before } = dateRangeBounds(range);
    if (from) conditions.push({ property, op: "gte", value: from });
    if (before) conditions.push({ property, op: "lt", value: before });
  }
  if (where) conditions.push(where);
  return conditions.length > 0 ? { and: conditions } : undefined;
}

function queryFrom(blueprint: string, search: CatalogSearch, size: number): EntityQuery {
  return {
    blueprint,
    q: search.q,
    filter: toFilter(search.f, search.d, search.where),
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
  const whereFilters = useWhereFilters(blueprint, search.where);
  const rowActions = useRowActions(
    blueprint,
    (page.data?.entities ?? []).map((entity) => entity.identifier),
  );

  if (!spec) return <UnpublishedBlueprint blueprint={blueprint} title={doc.title} />;

  const filters = search.f ?? {};
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

  const dates = search.d ?? {};
  const setDates = (key: string, range: DateRange) =>
    setSearch({ d: validDates({ ...dates, [key]: range }), page: undefined });
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
  // A link can filter by any property, so one without a field of its own
  // gets one while it's filtered, to show and change it.
  for (const id of Object.keys(filters)) {
    if (filterFields.some((field) => field.id === id)) continue;
    const property = spec.properties.find((p) => p.identifier === id);
    filterFields.push({
      kind: "options",
      id,
      title: property?.title ?? id,
      useOptions: () =>
        filters[id]?.map((value) => ({ value: JSON.stringify(value), label: String(value) })),
    });
  }
  for (const id of Object.keys(dates)) {
    if (filterFields.some((field) => field.id === id)) continue;
    const title = spec.properties.find((p) => p.identifier === id)?.title ?? id;
    filterFields.push({ kind: "date", id, title });
  }
  // Property values are keyed by their JSON, so values of any type fit the picker.
  const filterValue: DataTableFilters = {
    ...Object.fromEntries(
      Object.entries(filters).map(([id, values]) => [id, values.map((v) => JSON.stringify(v))]),
    ),
    ...dates,
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
      data={page.data?.entities ?? []}
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
        onChange: (id, value) => {
          if (filterFields.find((field) => field.id === id)?.kind === "date") {
            setDates(id, Array.isArray(value) ? {} : (value ?? {}));
            return;
          }
          const values = Array.isArray(value) ? value : [];
          setSearch({
            f: validFilters({
              ...filters,
              [id]: values.map((v) => JSON.parse(v) as unknown),
            }),
            page: undefined,
          });
        },
      }}
      fixedFilters={whereFilters.map(({ title, summary }, index) => ({
        title,
        summary,
        onRemove: () =>
          setSearch({
            where: search.where && withoutCondition(search.where, index),
            page: undefined,
          }),
      }))}
      onClearFilters={() =>
        setSearch({ f: undefined, d: undefined, where: undefined, page: undefined })
      }
      actions={
        // With no entities there are no columns to choose; a search or filter
        // that matches none keeps the menu, so the toolbar doesn't shift.
        (page.isPending ||
          (page.data?.total ?? 0) > 0 ||
          Boolean(search.q || search.f || search.d || search.where)) && (
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

  return values.map((value) => {
    const key = JSON.stringify(value);
    const facet = counts.get(key);
    const label = property.scorecard
      ? (tiers?.find((tier) => tier.name === value)?.title ?? "No tier")
      : typeof value === "boolean"
        ? value
          ? "Yes"
          : "No"
        : (facet?.title ?? String(value));
    return { value: key, label, count: facet?.count ?? (facets.data ? 0 : undefined) };
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
