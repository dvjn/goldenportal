import type { ReactNode } from "react";
import { TableCell, TableRow } from "@/components/ui/table";

export function MasterDetail({
  label,
  sidebar,
  children,
}: Readonly<{ label: string; sidebar: ReactNode; children: ReactNode }>) {
  return (
    <section className="overflow-hidden rounded-xl border bg-card shadow-xs">
      <div className="flex min-h-96 flex-col divide-y md:flex-row md:divide-x md:divide-y-0">
        <nav aria-label={label} className="shrink-0 space-y-0.5 overflow-y-auto p-2 md:w-64">
          {sidebar}
        </nav>
        <div className="min-w-0 flex-1">{children}</div>
      </div>
    </section>
  );
}

export function SidebarHeading({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <h2 className="px-3 pt-3 pb-1 text-xs font-medium text-muted-foreground first:pt-1">
      {children}
    </h2>
  );
}

export function SidebarItem({
  selected,
  onSelect,
  primary,
  secondary,
}: Readonly<{
  selected: boolean;
  onSelect: () => void;
  primary: ReactNode;
  secondary?: ReactNode;
}>) {
  return (
    <button
      type="button"
      aria-pressed={selected}
      onClick={onSelect}
      className="grid w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-3 rounded-md px-3 py-2 text-left text-sm leading-5 outline-none hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring aria-pressed:bg-accent aria-pressed:text-accent-foreground"
    >
      <span className="flex min-w-0 items-center gap-2 truncate">{primary}</span>
      <span className="flex shrink-0 items-center gap-2 text-xs leading-5 text-muted-foreground tabular-nums">
        {secondary}
      </span>
    </button>
  );
}

export function EmptyRow({ columns, message }: Readonly<{ columns: number; message: string }>) {
  return (
    <TableRow>
      <TableCell colSpan={columns} className="h-24 text-center text-muted-foreground">
        {message}
      </TableCell>
    </TableRow>
  );
}

export function DetailSection({
  title,
  description,
  children,
}: Readonly<{ title: string; description?: ReactNode; children: ReactNode }>) {
  return (
    <section aria-label={title} className="space-y-3">
      <div className="space-y-1">
        <h2 className="text-base font-semibold">{title}</h2>
        {description ? <p className="text-sm text-muted-foreground">{description}</p> : null}
      </div>
      {children}
    </section>
  );
}

export function DetailHeader({
  icon,
  title,
  meta,
  actions,
}: Readonly<{ icon?: ReactNode; title: string; meta?: ReactNode; actions?: ReactNode }>) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        {icon}
        <h2 className="text-xl font-semibold tracking-tight">{title}</h2>
        {meta}
      </div>
      {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
    </div>
  );
}

export function DetailEmpty({ message }: Readonly<{ message: string }>) {
  return <p className="py-12 text-center text-sm text-muted-foreground">{message}</p>;
}
