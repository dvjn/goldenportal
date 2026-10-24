import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";

type EntityLinkProps = {
  blueprint: string;
  identifier: string;
  children: ReactNode;
  className?: string;
};

// EntityLink opens an entity's page. Identifiers may hold slashes, so they
// go in the route's splat.
export function EntityLink({ blueprint, identifier, children, className }: EntityLinkProps) {
  return (
    <Link
      to="/catalog/$blueprint/$"
      params={{ blueprint, _splat: identifier }}
      className={className ?? "hover:underline"}
    >
      {children}
    </Link>
  );
}
