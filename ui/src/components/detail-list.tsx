import { cn } from "cn";
import type { ReactNode } from "react";

// DetailList is a card's labelled values, labels in a column of their own
// once there's room for one. Every row shares the grid, so the column fits
// the longest label.
export function DetailList({
  className,
  children,
}: Readonly<{ className?: string; children: ReactNode }>) {
  return (
    <dl
      className={cn(
        "grid items-start gap-x-4 gap-y-2 text-sm sm:grid-cols-[minmax(8rem,auto)_minmax(0,1fr)]",
        className,
      )}
    >
      {children}
    </dl>
  );
}

// Detail is one labelled value. help says more about it, under the value.
export function Detail({
  label,
  help,
  children,
}: Readonly<{ label: ReactNode; help?: string; children: ReactNode }>) {
  return (
    <div data-slot="detail" className="contents">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="grid min-w-0 gap-0.5">
        <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 [overflow-wrap:anywhere]">
          {children}
        </div>
        {help && <p className="text-xs text-muted-foreground">{help}</p>}
      </dd>
    </div>
  );
}
