import type { PaginationState, ReactTable, RowData } from "@tanstack/react-table";
import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { formatCount } from "@/lib/format";
import { type CursorLinks, type DataTableFeatures, pageSizes } from "./table";

type DataTablePaginationProps<TData extends RowData> = {
  table: ReactTable<DataTableFeatures, TData>;
  pagination: PaginationState;
  // total is how many rows match, across every page.
  total: number;
  // cursor pages by the cursors the page gives, which onCursor goes to;
  // none is the first page.
  cursor?: CursorLinks;
  onCursor?: (cursor?: string) => void;
};

export function DataTablePagination<TData extends RowData>({
  table,
  pagination: { pageIndex, pageSize },
  total: rows,
  cursor,
  onCursor = () => {},
}: Readonly<DataTablePaginationProps<TData>>) {
  const pages = Math.max(table.getPageCount(), 1);
  const shown = table.getRowModel().rows.length;
  const atStart = cursor ? !cursor.previous : !table.getCanPreviousPage();
  const atEnd = cursor ? !cursor.next : !table.getCanNextPage();

  const go = cursor
    ? {
        first: () => onCursor(undefined),
        previous: () => onCursor(cursor.previous),
        next: () => onCursor(cursor.next),
        last: () => onCursor(cursor.last),
      }
    : {
        first: () => table.firstPage(),
        previous: () => table.previousPage(),
        next: () => table.nextPage(),
        last: () => table.lastPage(),
      };
  const buttons = [
    { label: "First page", icon: ChevronsLeft, go: go.first, disabled: atStart },
    { label: "Previous page", icon: ChevronLeft, go: go.previous, disabled: atStart },
    { label: "Next page", icon: ChevronRight, go: go.next, disabled: atEnd },
    { label: "Last page", icon: ChevronsRight, go: go.last, disabled: atEnd },
  ];

  // A page read by cursor doesn't know where it starts, so no page says.
  const showing = `Showing ${formatCount(shown)} of ${formatCount(rows)}`;

  return (
    <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 text-sm">
      <span className="text-muted-foreground tabular-nums">{rows === 0 ? "No rows" : showing}</span>
      <div className="flex flex-wrap items-center gap-x-6 gap-y-2">
        <div className="flex items-center gap-2">
          <span className="text-muted-foreground">Rows per page</span>
          <Select
            value={String(pageSize)}
            onValueChange={(value) => table.setPageSize(Number(value))}
          >
            <SelectTrigger size="sm" className="w-18" aria-label="Rows per page">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {pageSizes.map((size) => (
                <SelectItem key={size} value={String(size)}>
                  {size}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        {!cursor && (
          <span className="tabular-nums">
            Page {formatCount(pageIndex + 1)} of {formatCount(pages)}
          </span>
        )}
        <div className="flex items-center gap-1">
          {buttons.map(({ label, icon: Icon, go, disabled }) => (
            <Button
              key={label}
              variant="outline"
              size="icon-sm"
              aria-label={label}
              onClick={go}
              disabled={disabled}
            >
              <Icon />
            </Button>
          ))}
        </div>
      </div>
    </div>
  );
}
