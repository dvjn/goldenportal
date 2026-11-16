import type { Column, RowData } from "@tanstack/react-table";
import { ArrowDown, ArrowUp, ChevronsUpDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { DataTableFeatures } from "./table";

type DataTableColumnHeaderProps<TData extends RowData, TValue> = {
  column: Column<DataTableFeatures, TData, TValue>;
  title: string;
};

// DataTableColumnHeader is a column title that sorts the table when clicked.
// Its cell says how the column is sorted, with ariaSort.
export function DataTableColumnHeader<TData extends RowData, TValue>({
  column,
  title,
}: DataTableColumnHeaderProps<TData, TValue>) {
  if (!column.getCanSort()) {
    return <span>{title}</span>;
  }

  const sorted = column.getIsSorted();
  const Icon = sorted === "asc" ? ArrowUp : sorted === "desc" ? ArrowDown : ChevronsUpDown;

  return (
    <Button
      variant="ghost"
      size="sm"
      className="-ml-2.5 h-8 data-[sorted]:text-foreground"
      data-sorted={sorted || undefined}
      onClick={column.getToggleSortingHandler()}
    >
      {title}
      <Icon className={sorted ? undefined : "text-muted-foreground"} aria-hidden />
    </Button>
  );
}

// ariaSort is how a column's header cell tells assistive tech it's sorted.
export function ariaSort<TData extends RowData, TValue>(
  column: Column<DataTableFeatures, TData, TValue>,
) {
  if (!column.getCanSort()) return undefined;
  const sorted = column.getIsSorted();
  return sorted === "asc" ? "ascending" : sorted === "desc" ? "descending" : "none";
}
