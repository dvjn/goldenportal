import { useNavigate } from "@tanstack/react-router";
import { type RowData, useTable } from "@tanstack/react-table";
import { cn } from "cn";
import { type ReactNode, useEffect, useState } from "react";
import { CardTitleBar } from "@/components/card-sections";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { writeUiState } from "@/hooks/use-ui-state";
import { DataTableFilterBox, type DataTableFixedFilter } from "./data-table-filter-box";
import type {
  DataTableFilterChange,
  DataTableFilterField,
  DataTableFilters,
} from "./data-table-filter-picker";
import { DataTablePagination } from "./data-table-pagination";
import {
  type DataTableColumn,
  dataTableFeatures,
  paginationFromSearch,
  paginationToSearch,
  sortingFromSearch,
  sortToSearch,
  type TableSearch,
  tableCellClass,
  usePageSize,
} from "./table";

type DataTableProps<TData extends RowData> = {
  // title heads a table that shares its page with other cards. A table that
  // fills its page or tab goes without, as the page names it.
  title?: string;
  data: TData[];
  columns: DataTableColumn<TData>[];
  getRowId: (row: TData) => string;
  // search is the route's table state; pass Route.useSearch().
  search: TableSearch;
  searchLabel: string;
  // filters sit in the search box as chips, added from a picker of their fields.
  filters?: {
    fields: DataTableFilterField[];
    value: DataTableFilters;
    onChange: DataTableFilterChange;
  };
  // fixedFilters are filters the table shows and can remove but not edit,
  // such as those a link brought along.
  fixedFilters?: DataTableFixedFilter[];
  // onClearFilters clears the filters besides the search.
  onClearFilters?: () => void;
  // actions sit at the end of the toolbar, e.g. a "New" button.
  actions?: ReactNode;
  // empty shows when there are no rows at all, before any search or filter.
  empty: ReactNode;
  // rowCount turns on server mode: data is already the current page, searched,
  // filtered and sorted by the server, and rowCount is how many rows match.
  rowCount?: number;
  // loading dims the rows while a new page is on its way.
  loading?: boolean;
  // pending shows placeholder rows until the first page arrives, so the table
  // doesn't claim to be empty while it loads.
  pending?: boolean;
  // error replaces the rows when the page couldn't be loaded.
  error?: Error | null;
  onRetry?: () => void;
};

const pendingRows = 5;

const searchDelay = 250;

// DataTable is a searchable, sortable, paged table whose state lives in the URL.
export function DataTable<TData extends RowData>({
  title,
  data,
  columns,
  getRowId,
  search,
  searchLabel,
  filters,
  fixedFilters,
  onClearFilters,
  actions,
  empty,
  rowCount,
  loading = false,
  pending = false,
  error,
  onRetry,
}: DataTableProps<TData>) {
  const navigate = useNavigate();
  const server = rowCount !== undefined;
  const sorting = sortingFromSearch(search.sort);
  usePageSize(search);
  const pagination = paginationFromSearch(search);

  function setSearch(patch: TableSearch) {
    // The table works on any route, so it merges into whatever search the route has.
    const search = (prev: TableSearch) => ({ ...prev, ...patch });
    void navigate({ to: ".", search: search as never, replace: true, resetScroll: false });
  }

  const table = useTable({
    features: dataTableFeatures,
    data,
    columns,
    getRowId,
    state: { sorting, pagination, globalFilter: search.q ?? "" },
    onSortingChange: (updater) => {
      const next = typeof updater === "function" ? updater(sorting) : updater;
      setSearch({ sort: sortToSearch(next), page: undefined });
    },
    onPaginationChange: (updater) => {
      const next = typeof updater === "function" ? updater(pagination) : updater;
      // A picked size is saved for every table, so it's left out of the URL.
      if (next.pageSize !== pagination.pageSize) writeUiState("pageSize", next.pageSize);
      setSearch(paginationToSearch(next));
    },
    autoResetPageIndex: false,
    manualPagination: server,
    manualSorting: server,
    manualFiltering: server,
    rowCount,
  });

  const [query, setQuery] = useState(search.q ?? "");
  useEffect(() => {
    if (query === (search.q ?? "")) return;
    const timer = setTimeout(
      () => setSearch({ q: query || undefined, page: undefined }),
      searchDelay,
    );
    return () => clearTimeout(timer);
  });

  const filtered = Object.keys(filters?.value ?? {}).length > 0 || Boolean(fixedFilters?.length);
  const searching = Boolean(search.q) || filtered;
  const total = server ? rowCount : table.getFilteredRowModel().rows.length;

  // A page past the end, after rows went away or a bookmark aged, goes back to the last one.
  const lastPage = Math.max(Math.ceil(total / pagination.pageSize), 1);
  const settled = !pending && !error;
  const pastEnd = settled && !loading && pagination.pageIndex + 1 > lastPage;
  useEffect(() => {
    if (pastEnd) setSearch({ page: lastPage > 1 ? lastPage : undefined });
  });

  function clearAll() {
    setQuery("");
    onClearFilters?.();
  }

  if (settled && total === 0 && !searching && (server || data.length === 0)) {
    return (
      <div className="flex flex-col gap-4">
        {actions && !title && <div className="flex justify-end">{actions}</div>}
        <Card className={title ? "gap-0 py-0 pt-(--card-spacing)" : "py-0"}>
          {title && <CardTitleBar title={title} action={actions} />}
          {empty}
        </Card>
      </div>
    );
  }

  const rows = table.getRowModel().rows;

  return (
    <Card
      className={cn("gap-0 py-0 shadow-xs", title && "pt-(--card-spacing)")}
      aria-busy={loading || pending || undefined}
    >
      {title && <CardTitleBar title={title} />}
      <div className="flex flex-wrap items-start justify-between gap-2 border-b px-(--card-spacing) py-3">
        <DataTableFilterBox
          label={searchLabel}
          query={query}
          onQueryChange={setQuery}
          fields={filters?.fields ?? []}
          value={filters?.value ?? {}}
          onChange={filters?.onChange ?? (() => {})}
          onClear={clearAll}
          fixed={fixedFilters}
        />
        {actions && <div className="flex items-center gap-2">{actions}</div>}
      </div>
      <Table className={loading ? "opacity-60 transition-opacity" : undefined}>
        <TableHeader>
          {table.getHeaderGroups().map((headerGroup) => (
            <TableRow key={headerGroup.id}>
              {headerGroup.headers.map((header) => (
                <TableHead key={header.id} className={tableCellClass}>
                  {header.isPlaceholder ? null : <table.FlexRender header={header} />}
                </TableHead>
              ))}
            </TableRow>
          ))}
        </TableHeader>
        <TableBody>
          {pending ? (
            Array.from({ length: pendingRows }, (_, row) => (
              // biome-ignore lint/suspicious/noArrayIndexKey: placeholder rows have no identity.
              <TableRow key={row}>
                {columns.map((_, cell) => (
                  // biome-ignore lint/suspicious/noArrayIndexKey: placeholder cells have no identity.
                  <TableCell key={cell} className={tableCellClass}>
                    <Skeleton className="h-4 w-full max-w-40" />
                  </TableCell>
                ))}
              </TableRow>
            ))
          ) : error ? (
            <TableRow>
              <TableCell colSpan={columns.length} className="h-24 text-center">
                <span className="text-muted-foreground">{error.message} </span>
                {onRetry && (
                  <Button variant="link" className="h-auto p-0" onClick={onRetry}>
                    Try again
                  </Button>
                )}
              </TableCell>
            </TableRow>
          ) : rows.length === 0 ? (
            <TableRow>
              <TableCell colSpan={columns.length} className="h-24 text-center">
                <span className="text-muted-foreground">
                  {search.q ? `Nothing matches "${search.q}". ` : "Nothing matches the filters. "}
                </span>
                <Button variant="link" className="h-auto p-0" onClick={clearAll}>
                  {filtered ? "Clear the filters" : "Clear the search"}
                </Button>
              </TableCell>
            </TableRow>
          ) : (
            rows.map((row) => (
              <TableRow key={row.id}>
                {row.getAllCells().map((cell) => (
                  <TableCell key={cell.id} className={tableCellClass}>
                    <table.FlexRender cell={cell} />
                  </TableCell>
                ))}
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
      {settled && (
        <div className="border-t px-(--card-spacing) py-3">
          <DataTablePagination table={table} pagination={pagination} total={total} />
        </div>
      )}
    </Card>
  );
}
