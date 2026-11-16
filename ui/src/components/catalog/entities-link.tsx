import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import type { Filter } from "@/features/catalog";

// EntitiesLink opens a blueprint's entities table filtered to filter alone.
// keep carries the viewer's columns and page size from a catalog page.
export function EntitiesLink({
  blueprint,
  filter,
  keep,
  label,
  className = "hover:underline",
  children,
}: Readonly<{
  blueprint: string;
  filter: Filter[];
  keep?: { cols?: string; size?: number };
  label?: string;
  className?: string;
  children: ReactNode;
}>) {
  return (
    <Link
      to="/catalog/$blueprint"
      params={{ blueprint }}
      search={{ cols: keep?.cols, size: keep?.size, view: "entities", filter }}
      aria-label={label}
      className={className}
    >
      {children}
    </Link>
  );
}
