import { useSearch } from "@tanstack/react-router";
import { cn } from "cn";
import type { ReactNode } from "react";
import { EntitiesLink } from "@/components/catalog/entities-link";

// ResultsPath is where a blueprint's entities hold a scorecard's result, or a check's.
export type ResultsPath = { blueprint: string; path: string };

// CountLink opens the entities a count counted; a count of none stays text.
export function CountLink({
  link,
  value,
  count,
  label,
  className,
  children,
}: Readonly<{
  link?: ResultsPath;
  value: string;
  count: number;
  label: string;
  className?: string;
  children: ReactNode;
}>) {
  const catalog = useSearch({ from: "/_app/catalog/$blueprint/", shouldThrow: false });
  if (!link || count === 0) return <span className={className}>{children}</span>;
  return (
    <EntitiesLink
      blueprint={link.blueprint}
      filter={[{ property: link.path, op: "eq", value }]}
      keep={{ cols: catalog?.cols, size: catalog?.size }}
      label={label}
      className={cn(className, "hover:underline")}
    >
      {children}
    </EntitiesLink>
  );
}
