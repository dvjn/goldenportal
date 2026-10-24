import type { Column, RowData } from "@tanstack/react-table";
import { ArrowDown, ArrowUp, ChevronsUpDown } from "lucide-react";
import { Button } from "@/components/ui/button";
import type { DataTableFeatures } from "./table";

type DataTableColumnHeaderProps<TData extends RowData, TValue> = {
  column: Column<DataTableFeatures, TData, TValue>;
  title: string;
};

// DataTableColumnHeader is a column title that sorts the table when clicked.
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
      <Icon className={sorted ? undefined : "text-muted-foreground"} />
      <span className="sr-only">
        {sorted === "asc" ? ", sorted ascending" : sorted === "desc" ? ", sorted descending" : ""}
      </span>
    </Button>
  );
}
