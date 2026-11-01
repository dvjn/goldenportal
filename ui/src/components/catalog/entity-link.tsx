import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";

type EntityLinkProps = {
  blueprint: string;
  identifier: string;
  // tab opens one of the page's tabs, such as a scorecard's result.
  tab?: string;
  children: ReactNode;
  className?: string;
};

// EntityLink opens an entity's page. Identifiers may hold slashes, so they
// go in the route's splat.
export function EntityLink({ blueprint, identifier, tab, children, className }: EntityLinkProps) {
  return (
    <Link
      to="/catalog/$blueprint/$"
      params={{ blueprint, _splat: identifier }}
      search={tab ? { tab } : {}}
      className={className ?? "hover:underline"}
    >
      {children}
    </Link>
  );
}
