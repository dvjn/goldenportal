import { cn } from "cn";
import { type ComponentProps, type ReactNode, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";

// ListItem is one value of a list: how it shows, its key, and the tint of
// its chip, if it has a color.
export type ListItem = { key: string; node: ReactNode; tint?: string };

// A table cell shows one item and lists the rest on demand, up to
// popoverItems; a page shows a few as chips and opens up to pageItems in
// place. Past those, viewAll takes over, to page through the rest.
const popoverItems = 10;
const chipItems = 6;
const pageItems = 50;

// ListValue shows a list of values without letting a long one take over:
// compact, for a table cell, it's the first item and a +N chip that lists
// the rest; otherwise it's chips that open up in place. viewAll links to
// where every item can be browsed. total counts every item, when items are
// only the first of them.
export function ListValue({
  items,
  total = items.length,
  compact = false,
  viewAll,
}: Readonly<{ items: ListItem[]; total?: number; compact?: boolean; viewAll?: ReactNode }>) {
  const [open, setOpen] = useState(false);
  if (compact) {
    const [first] = items;
    const rest = total - 1;
    return (
      <span className="flex min-w-0 items-center gap-1.5">
        <span className="min-w-0 truncate">{first?.node}</span>
        {rest > 0 && (
          <Popover>
            <PopoverTrigger asChild>
              <MoreChip count={rest} />
            </PopoverTrigger>
            <PopoverContent align="start" className="w-64 gap-0 p-1">
              <ul className="grid max-h-72 overflow-y-auto">
                {items.slice(0, popoverItems).map((item) => (
                  <li key={item.key} className="truncate rounded-md px-2 py-1.5 text-sm">
                    {item.node}
                  </li>
                ))}
              </ul>
              {total > popoverItems && viewAll && (
                <div className="border-t px-2 pt-1.5 pb-1 text-sm">{viewAll}</div>
              )}
            </PopoverContent>
          </Popover>
        )}
      </span>
    );
  }

  // Without somewhere to view them all, every item opens in place.
  const shown = items.slice(0, open ? (viewAll ? pageItems : items.length) : chipItems);
  return (
    <span className="flex flex-wrap items-center gap-1">
      {shown.map((item) => (
        <Badge key={item.key} variant="outline" className={cn("font-normal", item.tint)}>
          {item.node}
        </Badge>
      ))}
      {!open && total > shown.length && (
        <MoreChip count={total - shown.length} onClick={() => setOpen(true)} />
      )}
      {open && total > shown.length && viewAll && <span className="ml-1 text-sm">{viewAll}</span>}
    </span>
  );
}

// MoreChip counts the items not shown, and shows them when pressed.
function MoreChip({ count, ...props }: Readonly<{ count: number }> & ComponentProps<"button">) {
  return (
    <button
      type="button"
      aria-label={`Show ${count} more`}
      className="inline-flex h-5 shrink-0 items-center rounded-md bg-secondary px-1.5 text-xs text-secondary-foreground tabular-nums outline-none hover:bg-secondary/80 focus-visible:ring-2 focus-visible:ring-ring/50"
      {...props}
    >
      +{count}
    </button>
  );
}
