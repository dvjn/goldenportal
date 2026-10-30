import { useNavigate } from "@tanstack/react-router";
import { type RowData, useTable } from "@tanstack/react-table";
import { FilterX, Search } from "lucide-react";
import { type ReactNode, useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { DataTablePagination } from "./data-table-pagination";
import {
  type DataTableColumn,
  dataTableFeatures,
  paginationFromSearch,
  paginationToSearch,
  sortingFromSearch,
  sortToSearch,
  type TableSearch,
} from "./table";

type DataTableProps<TData extends RowData> = {
  data: TData[];
  columns: DataTableColumn<TData>[];
  getRowId: (row: TData) => string;
  // search is the route's table state; pass Route.useSearch().
  search: TableSearch;
  searchLabel: string;
  // filters sit next to the search box, e.g. faceted filters.
  filters?: ReactNode;
  // filtered says whether any filter besides the search is applied.
  filtered?: boolean;
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

// Cells keep a wider gutter at the table's edges than between columns.
const cellClass = "px-3 first:pl-5 last:pr-5";

const searchDelay = 250;

// DataTable is a searchable, sortable, paged table whose state lives in the URL.
export function DataTable<TData extends RowData>({
  data,
  columns,
  getRowId,
  search,
  searchLabel,
  filters,
  filtered = false,
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
        {actions && <div className="flex justify-end">{actions}</div>}
        <Card className="py-0">{empty}</Card>
      </div>
    );
  }

  const rows = table.getRowModel().rows;

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="relative w-full max-w-72">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={searchLabel}
            aria-label={searchLabel}
            className="pl-8"
          />
        </div>
        {actions && <div className="flex items-center gap-2">{actions}</div>}
      </div>
      {(filters || searching) && (
        <div className="flex flex-wrap items-center gap-2">
          {filters}
          {searching && (
            <Button variant="ghost" onClick={clearAll}>
              <FilterX />
              Reset
            </Button>
          )}
        </div>
      )}
      <Card className="gap-0 py-0 shadow-xs" aria-busy={loading || pending || undefined}>
        <Table className={loading ? "opacity-60 transition-opacity" : undefined}>
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => (
                  <TableHead key={header.id} className={cellClass}>
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
                    <TableCell key={cell} className={cellClass}>
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
                    <TableCell key={cell.id} className={cellClass}>
                      <table.FlexRender cell={cell} />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
        {settled && (
          <div className="border-t px-5 py-3">
            <DataTablePagination table={table} pagination={pagination} total={total} />
          </div>
        )}
      </Card>
    </div>
  );
}
