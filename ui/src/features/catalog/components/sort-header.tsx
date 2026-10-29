import { ChevronDownIcon, ChevronsUpDownIcon, ChevronUpIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { TableHead } from "@/components/ui/table";
import { cn } from "@/lib/utils";
import type { SortDirection } from "../api";

const ariaSort = { asc: "ascending", desc: "descending" } as const;

export function SortHeader({
  label,
  direction,
  onSort,
  align = "left",
  className,
}: Readonly<{
  label: ReactNode;
  direction: SortDirection | undefined;
  onSort: () => void;
  align?: "left" | "right";
  className?: string;
}>) {
  return (
    <TableHead
      aria-sort={direction ? ariaSort[direction] : "none"}
      className={cn(align === "right" && "text-right", className)}
    >
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className={cn("font-medium", align === "right" ? "-mr-2.5" : "-ml-2.5")}
        aria-pressed={direction !== undefined}
        onClick={onSort}
      >
        {label}
        {direction === "asc" ? <ChevronUpIcon aria-hidden="true" /> : null}
        {direction === "desc" ? <ChevronDownIcon aria-hidden="true" /> : null}
        {direction === undefined ? (
          <ChevronsUpDownIcon aria-hidden="true" className="text-muted-foreground" />
        ) : null}
      </Button>
    </TableHead>
  );
}
