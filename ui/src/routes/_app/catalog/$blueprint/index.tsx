import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { Columns3, FileCode, LayoutGrid, RotateCcw, Rows3 } from "lucide-react";
import type { ReactNode } from "react";
import { RowActions, useRowActions } from "@/components/actions/entity-actions";
import { BlueprintIcon, IconTile } from "@/components/catalog/blueprint-icon";
import { useCatalogFilters } from "@/components/catalog/catalog-filters";
import { EntityLink } from "@/components/catalog/entity-link";
import { EmptyValue, PropertyValue } from "@/components/catalog/property-value";
import { SplitTitle } from "@/components/catalog/split-title";
import { UnpublishedBlueprint } from "@/components/catalog/unpublished-blueprint";
import {
  DataTable,
  DataTableColumnHeader,
  dataTableColumns,
  pageSize,
  type TableSearch,
  useCursorPage,
  usePageSize,
  validateTableSearch,
} from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { PageBreadcrumbs } from "@/components/page-breadcrumbs";
import { PageError } from "@/components/page-error";
import { PageHeader } from "@/components/page-header";
import { pageIcons } from "@/components/page-icons";
import { PageTab } from "@/components/page-tabs";
import { ScorecardResults } from "@/components/scorecards/scorecard-results";
import { Truncated } from "@/components/truncated";
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
} from "@/features/catalog";
import { publishedGradedBy, scorecardPath, scorecards, scorecardView } from "@/features/scorecards";
import { formatAbsolute, formatCount, formatRelative } from "@/lib/format";
import { usePageTitle } from "@/lib/page-title";

// CatalogSearch keeps filter while a scorecard's tab is open, for the entities tab to come back to.
type CatalogSearch = TableSearch & {
  view?: string;
  filter?: Filter[];
  cols?: string;
};

const text = (value: unknown) => (typeof value === "string" && value !== "" ? value : undefined);

function validateCatalogSearch(search: Record<string, unknown>): CatalogSearch {
  // Both tables page by cursor.
  const { page: _page, ...table } = validateTableSearch(search);
  return {
    ...table,
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

const isScorecardView = (view?: string) => view?.startsWith(scorecardView("")) ?? false;

// entitySearch is the entities table's part of the search. A scorecard's
// results table has the sorting and paging while its tab is open, so the
// entities, which the tab still counts, are left unsearched and unsorted.
function entitySearch({ view, ...search }: CatalogSearch): CatalogSearch {
  if (!isScorecardView(view)) return search;
  const { q: _q, sort: _s, cursor: _c, ...rest } = search;
  return rest;
}

function queryFrom(blueprint: string, search: CatalogSearch, size: number): EntityQuery {
  return {
    blueprint,
    q: search.q,
    filter: search.filter && { and: search.filter },
    sort: search.sort,
    cursor: search.cursor,
    size,
  };
}

export const Route = createFileRoute("/_app/catalog/$blueprint/")({
  validateSearch: validateCatalogSearch,
  loaderDeps: ({ search }) => entitySearch(search),
  loader: ({ context, params, deps }) => {
    const query = queryFrom(params.blueprint, deps, pageSize(deps));
    return Promise.all([
      context.queryClient.ensureQueryData(blueprints.queries.detail(params.blueprint)),
      context.queryClient.ensureQueryData(scorecards.queries.list),
      // A filter, sort or cursor the server turns down shows in the table, not in place of the page.
      context.queryClient.prefetchQuery(entitiesQueryOptions({ ...query, cursor: undefined })),
      context.queryClient.prefetchQuery(entitiesQueryOptions(query)),
    ]);
  },
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

// Numbers sort largest first; text, dates and tiers, from the bottom of
// their ladder, ascending.
const largestFirst = (property: Property) =>
  !property.scorecard && (property.type === "number" || property.type === "integer");

// builtInColumns are the fields every entity has that can be columns,
// besides the title column, which shows the title and identifier.
const builtInColumns = [
  { id: "connection", title: "Connection" },
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
  const entities = entitySearch(search);
  const query = queryFrom(blueprint, entities, usePageSize(entities));
  const { page, first, total, pending } = useCursorPage(
    (cursor) => entitiesQueryOptions({ ...query, cursor }),
    query.cursor,
  );
  // Only published scorecards have results to show.
  const { data: scorecardList } = useSuspenseQuery(scorecards.queries.list);
  const graded = publishedGradedBy(scorecardList, blueprint);
  const conditions = search.filter ?? [];
  const setSearch = (patch: Partial<CatalogSearch>) =>
    navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true, resetScroll: false });
  const filters = useCatalogFilters(blueprint, spec, conditions, (next) =>
    setSearch({ filter: next.length > 0 ? next : undefined, cursor: undefined }),
  );
  const rowActions = useRowActions(
    blueprint,
    (page.data?.items ?? []).map((entity) => entity.identifier),
  );

  if (!spec) return <UnpublishedBlueprint blueprint={blueprint} title={doc.title} />;

  const shown = search.cols?.split(",") ?? defaultColumns(spec);

  // The subtitle the entity page shows also stands under each title.
  const subtitle = spec.properties.find(
    (property) => property.identifier === spec.entity?.header?.subtitle,
  );
  const columns = [
    column.accessor("title", {
      id: "title",
      header: ({ column }) => <DataTableColumnHeader column={column} title="Title" />,
      cell: ({ row }) => (
        <div className="grid max-w-72 min-w-40">
          <Truncated title={row.original.title} className="max-w-none font-medium">
            <EntityLink
              blueprint={blueprint}
              identifier={row.original.identifier}
              className="hover:underline"
            >
              <SplitTitle title={row.original.title} />
            </EntityLink>
          </Truncated>
          {subtitle ? (
            <span className="min-w-0 text-xs text-muted-foreground">
              <PropertyValue
                property={subtitle}
                value={row.original.properties[subtitle.identifier]}
                links={row.original.related[subtitle.identifier]}
                compact
              />
            </span>
          ) : (
            row.original.title !== row.original.identifier && (
              <Truncated className="max-w-none font-mono text-xs text-muted-foreground">
                {row.original.identifier}
              </Truncated>
            )
          )}
        </div>
      ),
    }),
    // Columns keep the order they're listed in.
    ...shown.flatMap((id) => {
      const builtIn = builtInColumns.find((c) => c.id === id);
      if (builtIn) {
        return [
          column.accessor((entity): unknown => entity[builtIn.id as "connection"], {
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
          sortDescFirst: largestFirst(property),
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

  // The blueprint's own tabs come first, then the entities table every
  // catalog page has, then its scorecards' results, so the first tab is the
  // one that opens.
  const views = spec.catalog?.tabs ?? [];
  const view =
    views.some((v) => v.identifier === search.view) ||
    graded.some((s) => scorecardView(s.identifier) === search.view)
      ? (search.view as string)
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
      rowCount={total}
      cursor={page.data ?? {}}
      loading={page.isPlaceholderData}
      pending={pending}
      error={page.error}
      onRetry={() => void page.refetch()}
      filters={filters.filters}
      onClearFilters={filters.onClearFilters}
      actions={
        // With no entities there are no columns to choose; a search, filter
        // or sort that matches none, or that fails, keeps the menu, so the
        // toolbar doesn't shift.
        (page.isPending ||
          page.isError ||
          total > 0 ||
          Boolean(search.q || search.filter || search.sort)) && (
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
          description="They show up here once a connection writes them."
        />
      }
    />
  );

  if (views.length === 0 && graded.length === 0) {
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
        navigate({
          search: (prev) =>
            // A scorecard's results and the entities share the table's
            // search, sort and page, which don't carry from one to another.
            isScorecardView(prev.view) || isScorecardView(next)
              ? {
                  ...prev,
                  view: next,
                  q: undefined,
                  sort: undefined,
                  cursor: undefined,
                }
              : { ...prev, view: next },
          replace: true,
        })
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
            {first.data && (
              <span className="text-xs text-muted-foreground tabular-nums">
                {formatCount(total)}
              </span>
            )}
          </PageTab>
          {graded.map((s) => (
            <PageTab key={s.identifier} value={scorecardView(s.identifier)}>
              {s.icon ? <BlueprintIcon icon={s.icon} /> : <pageIcons.scorecards aria-hidden />}
              {s.title}
            </PageTab>
          ))}
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
      {graded.map((s) => (
        <TabsContent key={s.identifier} value={scorecardView(s.identifier)} className="pt-2">
          <ScorecardResults
            identifier={s.identifier}
            search={search}
            path={scorecardPath(spec.properties, s.identifier).path}
          />
        </TabsContent>
      ))}
      <TabsContent value="entities" className="pt-2">
        {table}
      </TabsContent>
    </Tabs>
  );
}

// BuiltInCell shows a field every entity has: dates as how long ago, and
// the connection that wrote it.
function BuiltInCell({ id, value }: Readonly<{ id: string; value: string }>) {
  if (!value) return <EmptyValue />;
  if (id === "connection") return <span className="text-muted-foreground">{value}</span>;
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
              Reset to Defaults
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
