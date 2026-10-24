import { keepPreviousData, useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { Columns3, LayoutGrid } from "lucide-react";
import { BlueprintIcon } from "@/components/catalog/blueprint-icon";
import { EntityLink } from "@/components/catalog/entity-link";
import { PropertyValue } from "@/components/catalog/property-value";
import {
  DataTable,
  DataTableColumnHeader,
  DataTableFacetedFilter,
  dataTableColumns,
  type TableSearch,
  validateTableSearch,
} from "@/components/data-table";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { type BlueprintSpec, blueprints, type Property } from "@/features/blueprints";
import {
  type Entity,
  type EntityQuery,
  entitiesQueryOptions,
  type Filter,
  facetsQueryOptions,
} from "@/features/catalog";
import { formatAbsolute, formatRelative } from "@/lib/format";

// Filters maps a property to the values it's filtered to.
type Filters = Record<string, (string | number | boolean)[]>;

// CatalogSearch is the table's state in the URL; cols lists the shown columns.
type CatalogSearch = TableSearch & { f?: Filters; cols?: string };

function validateCatalogSearch(search: Record<string, unknown>): CatalogSearch {
  return {
    ...validateTableSearch(search),
    f: validFilters(search.f),
    cols: typeof search.cols === "string" && search.cols !== "" ? search.cols : undefined,
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

// toFilter turns the picked values into the server's filter: each property
// matches any of its values, and every property must match.
function toFilter(filters: Filters | undefined): Filter | undefined {
  const conditions = Object.entries(filters ?? {}).map(([property, values]) => ({
    property,
    op: "in",
    values,
  }));
  return conditions.length > 0 ? { and: conditions } : undefined;
}

function queryFrom(blueprint: string, search: CatalogSearch): EntityQuery {
  return {
    blueprint,
    q: search.q,
    filter: toFilter(search.f),
    sort: search.sort,
    page: search.page,
    size: search.size,
  };
}

export const Route = createFileRoute("/_app/catalog/$blueprint/")({
  validateSearch: validateCatalogSearch,
  loaderDeps: ({ search }) => search,
  loader: ({ context, params, deps }) =>
    Promise.all([
      context.queryClient.ensureQueryData(blueprints.queries.detail(params.blueprint)),
      context.queryClient.ensureQueryData(entitiesQueryOptions(queryFrom(params.blueprint, deps))),
    ]),
  component: BlueprintCatalogPage,
});

// Lookups follow relations at read time, and objects and lists don't order,
// so the server can't sort by them.
function sortable(property: Property) {
  return (
    !property.lookup && !property.many && property.type !== "array" && property.type !== "object"
  );
}

// filterable properties get a filter with their values to pick from.
function filterable(property: Property) {
  if (property.lookup) return false;
  return (
    Boolean(property.relation) ||
    Boolean(property.enum) ||
    property.type === "boolean" ||
    (property.type === "array" && property.items?.type === "string")
  );
}

// defaultColumns are the first few properties that fit in a cell.
function defaultColumns(spec: BlueprintSpec) {
  return spec.properties
    .filter((property) => property.type !== "object")
    .slice(0, 5)
    .map((property) => property.identifier);
}

const column = dataTableColumns<Entity>();

function BlueprintCatalogPage() {
  const { blueprint } = Route.useParams();
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { data: doc } = useSuspenseQuery(blueprints.queries.detail(blueprint));
  const spec = doc.published?.spec;
  const page = useQuery({
    ...entitiesQueryOptions(queryFrom(blueprint, search)),
    placeholderData: keepPreviousData,
  });

  if (!spec) {
    return (
      <Empty>
        <EmptyHeader>
          <EmptyTitle>{doc.title} isn't published yet</EmptyTitle>
          <EmptyDescription>Its entities show up here once it is.</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  }

  const filters = search.f ?? {};
  const shown = search.cols?.split(",") ?? defaultColumns(spec);
  const setSearch = (patch: Partial<CatalogSearch>) =>
    navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true, resetScroll: false });

  const columns = [
    column.accessor("title", {
      id: "$title",
      header: ({ column }) => <DataTableColumnHeader column={column} title="Title" />,
      cell: ({ row }) => (
        <EntityLink
          blueprint={blueprint}
          identifier={row.original.identifier}
          className="grid hover:underline"
        >
          <span className="font-medium">{row.original.title}</span>
          {row.original.title !== row.original.identifier && (
            <code className="font-mono text-xs text-muted-foreground">
              {row.original.identifier}
            </code>
          )}
        </EntityLink>
      ),
    }),
    ...spec.properties
      .filter((property) => shown.includes(property.identifier))
      .map((property) =>
        column.accessor((entity) => entity.properties[property.identifier], {
          id: property.identifier,
          enableSorting: sortable(property),
          header: ({ column }) => <DataTableColumnHeader column={column} title={property.title} />,
          cell: ({ row }) => (
            <PropertyValue
              property={property}
              value={row.original.properties[property.identifier]}
              links={row.original.related[property.identifier]}
            />
          ),
        }),
      ),
    column.accessor("updatedAt", {
      id: "$updatedAt",
      header: ({ column }) => <DataTableColumnHeader column={column} title="Updated" />,
      cell: ({ getValue }) => (
        <span
          className="whitespace-nowrap text-muted-foreground"
          title={formatAbsolute(getValue())}
        >
          {formatRelative(getValue())}
        </span>
      ),
    }),
  ];

  const filtered = Object.keys(filters).length > 0;

  return (
    <div className="flex flex-col gap-6">
      <div className="grid gap-3">
        <Breadcrumb>
          <BreadcrumbList>
            <BreadcrumbItem>
              <BreadcrumbLink asChild>
                <Link to="/catalog">Catalog</Link>
              </BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator />
            <BreadcrumbItem>
              <BreadcrumbPage>{spec.title}</BreadcrumbPage>
            </BreadcrumbItem>
          </BreadcrumbList>
        </Breadcrumb>
        <div className="flex items-center gap-3">
          <span className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted">
            <BlueprintIcon icon={spec.icon} className="size-5" />
          </span>
          <div className="grid">
            <h1 className="text-2xl font-semibold tracking-tight">{spec.title}</h1>
            <p className="text-sm text-muted-foreground">
              {spec.description}
              {spec.description && page.data && " · "}
              {page.data && `${page.data.total} ${page.data.total === 1 ? "entity" : "entities"}`}
            </p>
          </div>
        </div>
      </div>
      <DataTable
        data={page.data?.entities ?? []}
        columns={columns}
        getRowId={(entity) => entity.identifier}
        search={search}
        searchLabel={`Search ${spec.title.toLowerCase()}`}
        rowCount={page.data?.total ?? 0}
        loading={page.isPlaceholderData}
        filters={spec.properties.filter(filterable).map((property) => (
          <PropertyFilter
            key={property.identifier}
            blueprint={blueprint}
            property={property}
            selected={(filters[property.identifier] ?? []).map((value) => JSON.stringify(value))}
            onChange={(values) =>
              setSearch({
                f: validFilters({
                  ...filters,
                  [property.identifier]: values.map((value) => JSON.parse(value) as unknown),
                }),
                page: undefined,
              })
            }
          />
        ))}
        filtered={filtered}
        onClearFilters={() => setSearch({ f: undefined, page: undefined })}
        actions={
          <ColumnsMenu
            spec={spec}
            shown={shown}
            onChange={(cols) => setSearch({ cols: cols.join(",") })}
          />
        }
        empty={
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <LayoutGrid />
              </EmptyMedia>
              <EmptyTitle>No {spec.title.toLowerCase()} entities yet</EmptyTitle>
              <EmptyDescription>
                They show up here once an integration writes them.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        }
      />
    </div>
  );
}

// PropertyFilter picks values of one property. Enums and booleans offer
// their values; relations and tag lists offer what entities actually have.
// Options are keyed by their JSON, so values of any type fit the picker.
function PropertyFilter({
  blueprint,
  property,
  selected,
  onChange,
}: Readonly<{
  blueprint: string;
  property: Property;
  selected: string[];
  onChange: (values: string[]) => void;
}>) {
  const fixed = property.enum ?? (property.type === "boolean" ? [true, false] : undefined);
  const facets = useQuery({
    ...facetsQueryOptions(blueprint, property.identifier),
    enabled: !fixed || property.type === "boolean",
  });
  const counts = new Map(facets.data?.map((facet) => [JSON.stringify(facet.value), facet]));
  const values = fixed ?? facets.data?.map((facet) => facet.value) ?? [];

  const options = values.map((value) => {
    const key = JSON.stringify(value);
    const facet = counts.get(key);
    const label =
      typeof value === "boolean" ? (value ? "Yes" : "No") : (facet?.title ?? String(value));
    return { value: key, label, count: facet?.count ?? (facets.data ? 0 : undefined) };
  });

  return (
    <DataTableFacetedFilter
      title={property.title}
      options={options}
      selected={selected}
      onChange={onChange}
    />
  );
}

function ColumnsMenu({
  spec,
  shown,
  onChange,
}: Readonly<{ spec: BlueprintSpec; shown: string[]; onChange: (cols: string[]) => void }>) {
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
        {spec.properties.map((property) => (
          <DropdownMenuCheckboxItem
            key={property.identifier}
            checked={shown.includes(property.identifier)}
            onSelect={(event) => event.preventDefault()}
            onCheckedChange={(checked) =>
              onChange(
                spec.properties
                  .map((p) => p.identifier)
                  .filter((id) => (id === property.identifier ? checked : shown.includes(id))),
              )
            }
          >
            {property.title}
          </DropdownMenuCheckboxItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
