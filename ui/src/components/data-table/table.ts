import {
  type ColumnDef,
  columnFilteringFeature,
  createColumnHelper,
  createFilteredRowModel,
  createPaginatedRowModel,
  createSortedRowModel,
  filterFns,
  globalFilteringFeature,
  type PaginationState,
  type RowData,
  rowPaginationFeature,
  rowSortingFeature,
  type SortingState,
  sortFns,
  tableFeatures,
} from "@tanstack/react-table";
import { readUiState, useUiState } from "@/hooks/use-ui-state";

// The features every data table has: sorting, a search box and pages, all client-side.
export const dataTableFeatures = tableFeatures({
  rowSortingFeature,
  sortedRowModel: createSortedRowModel(),
  sortFns,
  columnFilteringFeature,
  globalFilteringFeature,
  filteredRowModel: createFilteredRowModel(),
  filterFns,
  rowPaginationFeature,
  paginatedRowModel: createPaginatedRowModel(),
});

export type DataTableFeatures = typeof dataTableFeatures;

// biome-ignore lint/suspicious/noExplicitAny: columns of one table hold different value types.
export type DataTableColumn<TData extends RowData> = ColumnDef<DataTableFeatures, TData, any>;

export function dataTableColumns<TData extends RowData>() {
  return createColumnHelper<DataTableFeatures, TData>();
}

// tableCellClass gives a table in a card the card's gutter at its edges and a
// narrower one between columns; plain tables use it to match DataTable.
export const tableCellClass = "px-3 first:pl-(--card-spacing) last:pr-(--card-spacing)";

export const pageSizes = [10, 20, 50, 100] as const;
const defaultPageSize = 10;

// savedPageSize is the page size last picked on any table.
function savedPageSize(): number {
  const size = readUiState("pageSize", defaultPageSize);
  return (pageSizes as readonly number[]).includes(size) ? size : defaultPageSize;
}

// pageSize is how many rows a table shows: what its URL says, or else the
// size last picked. Loaders read it; components use usePageSize, since a
// pick on the first page leaves the URL as it was.
export function pageSize(search: TableSearch): number {
  return search.size ?? savedPageSize();
}

// usePageSize is pageSize, rerendering when another size is picked.
export function usePageSize(search: TableSearch): number {
  useUiState("pageSize", defaultPageSize);
  return pageSize(search);
}

// TableSearch is a table's state in the URL: ?q=…&sort=-name&page=2&size=50.
// A picked page size is saved instead, so size is only in a link shared.
export type TableSearch = {
  q?: string;
  sort?: string;
  page?: number;
  size?: number;
};

// validateTableSearch reads table state from a route's search params, dropping anything invalid.
export function validateTableSearch(search: Record<string, unknown>): TableSearch {
  const page = Number(search.page);
  const size = Number(search.size);
  return {
    q: typeof search.q === "string" && search.q !== "" ? search.q : undefined,
    sort: typeof search.sort === "string" && search.sort !== "" ? search.sort : undefined,
    page: Number.isInteger(page) && page > 1 ? page : undefined,
    size: (pageSizes as readonly number[]).includes(size) ? size : undefined,
  };
}

export function sortingFromSearch(sort: string | undefined): SortingState {
  if (!sort) return [];
  const desc = sort.startsWith("-");
  return [{ id: desc ? sort.slice(1) : sort, desc }];
}

export function sortToSearch(sorting: SortingState): string | undefined {
  const [first] = sorting;
  if (!first) return undefined;
  return first.desc ? `-${first.id}` : first.id;
}

export function paginationFromSearch(search: TableSearch): PaginationState {
  return { pageIndex: (search.page ?? 1) - 1, pageSize: pageSize(search) };
}

export function paginationToSearch(
  pagination: PaginationState,
): Pick<TableSearch, "page" | "size"> {
  return {
    page: pagination.pageIndex > 0 ? pagination.pageIndex + 1 : undefined,
    size: pagination.pageSize !== savedPageSize() ? pagination.pageSize : undefined,
  };
}
