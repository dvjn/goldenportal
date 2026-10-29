import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { PlusIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { ListSkeleton } from "@/components/common/list-skeleton";
import { PageHeader } from "@/components/common/page-header";
import { PaginationBar } from "@/components/common/pagination-bar";
import { PermissionGate } from "@/components/common/permission-gate";
import { StateCard } from "@/components/common/state-card";
import { Button } from "@/components/ui/button";
import { usePageTitle } from "@/lib/use-page-title";
import { cn } from "@/lib/utils";
import { type Blueprint, entityPageSize, isNotFound } from "../api";
import { columnOptions, resolveColumns } from "../columns";
import { blueprintQueryOptions, entityListQueryOptions, useSourceIcons } from "../queries";
import { type CatalogSearch, clearedFilters, searchFilters } from "../search";
import { BlueprintIconTile } from "./blueprint-icon";
import { CatalogToolbar } from "./catalog-toolbar";
import { type EntitySort, EntityTable } from "./entity-table";
import { SourceMark } from "./source-mark";

const searchDebounceMs = 300;

type SearchChange = (next: Partial<CatalogSearch>) => void;

export function CatalogView({
  blueprintId,
  search,
  onSearchChange,
}: Readonly<{
  blueprintId: string;
  search: CatalogSearch;
  onSearchChange: SearchChange;
}>) {
  const blueprint = useQuery(blueprintQueryOptions(blueprintId));

  if (isNotFound(blueprint.error)) {
    return (
      <StateCard
        title="Blueprint not found"
        message={`No blueprint has the identifier "${blueprintId}".`}
      >
        <Button asChild variant="outline" size="sm">
          <Link to="/">Back to the catalog</Link>
        </Button>
      </StateCard>
    );
  }
  if (blueprint.isError) {
    return <StateCard title="Catalog unavailable" message="We could not load the blueprint." />;
  }
  if (!blueprint.data) return <ListSkeleton />;

  return (
    <CatalogList
      key={blueprint.data.identifier}
      blueprint={blueprint.data}
      search={search}
      onSearchChange={onSearchChange}
    />
  );
}

export function resolveSort(blueprint: Blueprint, search: CatalogSearch): EntitySort {
  if (!search.sort) return blueprint.table.sort;
  return { by: search.sort, direction: search.dir ?? "asc" };
}

function CatalogList({
  blueprint,
  search,
  onSearchChange,
}: Readonly<{
  blueprint: Blueprint;
  search: CatalogSearch;
  onSearchChange: SearchChange;
}>) {
  usePageTitle(blueprint.title);
  const q = search.q ?? "";
  const [searchText, setSearchText] = useState(q);
  const sources = useSourceIcons();

  const filterKeys = blueprint.table.filters;
  const filters = searchFilters(search, filterKeys);
  const sort = resolveSort(blueprint, search);
  const page = search.page ?? 1;
  const pageSize = search.pageSize ?? entityPageSize;
  const visibleColumns = search.columns ?? blueprint.table.columns;
  const entities = useQuery(
    entityListQueryOptions(blueprint.identifier, {
      q: q || undefined,
      integration: search.integration,
      sort: sort.by,
      dir: sort.direction,
      page,
      pageSize,
      filters,
    }),
  );
  const total = entities.data?.total;
  const filtered = q !== "" || search.integration !== undefined || Object.keys(filters).length > 0;

  useEffect(() => {
    if (searchText === q) return;
    const timer = setTimeout(
      () => onSearchChange({ q: searchText || undefined, page: undefined }),
      searchDebounceMs,
    );
    return () => clearTimeout(timer);
  }, [searchText, q, onSearchChange]);

  useEffect(() => {
    if (total === undefined || total === 0) return;
    const lastPage = Math.ceil(total / pageSize);
    if (page > lastPage) onSearchChange({ page: lastPage });
  }, [total, page, pageSize, onSearchChange]);

  function clearFilters() {
    setSearchText("");
    onSearchChange({
      ...clearedFilters(filterKeys),
      q: undefined,
      integration: undefined,
      page: undefined,
    });
  }

  let body = <ListSkeleton />;
  if (entities.isError) {
    body = (
      <StateCard title="Entities unavailable" message="We could not load the entities.">
        <Button type="button" variant="outline" size="sm" onClick={() => void entities.refetch()}>
          Retry
        </Button>
      </StateCard>
    );
  } else if (entities.data && entities.data.entities.length === 0) {
    body = filtered ? (
      <StateCard title="No entities match" message="Try a different search or fewer filters.">
        <Button type="button" variant="outline" size="sm" onClick={clearFilters}>
          Clear filters
        </Button>
      </StateCard>
    ) : (
      <StateCard
        title="No entities yet"
        message={`Entities of type ${blueprint.title.toLowerCase()} appear here once they exist.`}
      />
    );
  } else if (entities.data) {
    body = (
      <div
        aria-busy={entities.isFetching}
        className={cn("transition-opacity", entities.isPlaceholderData && "opacity-60")}
      >
        <EntityTable
          blueprint={blueprint}
          entities={entities.data.entities}
          columns={resolveColumns(blueprint, visibleColumns)}
          sort={sort}
          onSortChange={(next) =>
            onSearchChange({ sort: next.by, dir: next.direction, page: undefined })
          }
          footer={
            <PaginationBar
              page={page}
              pageSize={pageSize}
              total={entities.data.total}
              onPageChange={(next) => onSearchChange({ page: next })}
              onPageSizeChange={(next) => onSearchChange({ pageSize: next, page: undefined })}
            />
          }
        />
      </div>
    );
  }

  return (
    <>
      <PageHeader
        title={blueprint.title}
        description={blueprint.description}
        icon={<BlueprintIconTile icon={blueprint.icon} />}
        actions={
          <>
            <SourceMark
              icon={sources.data?.ofBlueprint(blueprint.identifier) ?? ""}
              className="size-5"
            />
            <Button asChild variant="outline">
              <Link
                to="/settings/catalog-setup"
                search={{ tab: "model", blueprint: blueprint.identifier }}
              >
                View definition
              </Link>
            </Button>
            <PermissionGate permission="catalog.write" fallback={null}>
              <Button asChild>
                <Link to="/catalog/$blueprint/new" params={{ blueprint: blueprint.identifier }}>
                  <PlusIcon aria-hidden="true" />
                  New entity
                </Link>
              </Button>
            </PermissionGate>
          </>
        }
      />
      <div className="flex flex-col gap-3">
        <CatalogToolbar
          blueprint={blueprint}
          searchText={searchText}
          onSearchTextChange={setSearchText}
          filters={filters}
          onFilterChange={(key, filter) => onSearchChange({ [key]: filter, page: undefined })}
          integrations={entities.data?.integrations ?? []}
          sources={sources.data}
          integration={search.integration}
          onIntegrationChange={(integration) => onSearchChange({ integration, page: undefined })}
          filtered={filtered}
          onClearFilters={clearFilters}
          total={total}
          columnOptions={columnOptions(blueprint)}
          visibleColumns={visibleColumns}
          onVisibleColumnsChange={(columns) => onSearchChange({ columns })}
          onResetColumns={() => onSearchChange({ columns: undefined })}
        />
        {body}
      </div>
    </>
  );
}
