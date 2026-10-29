import { cn } from "cn";
import type { ReactNode } from "react";

export function DescriptionList({
  className,
  children,
}: Readonly<{ className?: string; children: ReactNode }>) {
  return <dl className={cn("grid gap-3", className)}>{children}</dl>;
}

export function DescriptionRow({
  label,
  children,
}: Readonly<{ label: ReactNode; children: ReactNode }>) {
  return (
    <div className="grid grid-cols-[10rem_minmax(0,1fr)] items-baseline gap-2 text-sm">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 break-words">{children}</dd>
    </div>
  );
}
