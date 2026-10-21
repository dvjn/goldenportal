import { Link, useMatches } from "@tanstack/react-router";
import { Fragment } from "react";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";

declare module "@tanstack/react-router" {
  interface StaticDataRouteOption {
    breadcrumb?: string;
  }
}

type Crumb = { path: string; label: string };

const home: Crumb = { path: "/", label: "Home" };

export function PageBreadcrumbs() {
  const crumbs = useMatches({
    select: (matches) =>
      matches.flatMap(({ pathname, staticData }) =>
        staticData.breadcrumb ? [{ path: pathname, label: staticData.breadcrumb }] : [],
      ),
  });
  const trail = [home, ...crumbs];

  if (trail.length < 2) return null;

  return (
    <Breadcrumb>
      <BreadcrumbList>
        {trail.map((crumb, index) => (
          <Fragment key={crumb.path}>
            {index > 0 && <BreadcrumbSeparator />}
            <BreadcrumbItem>
              {index === trail.length - 1 ? (
                <BreadcrumbPage>{crumb.label}</BreadcrumbPage>
              ) : (
                <BreadcrumbLink asChild>
                  <Link to={crumb.path}>{crumb.label}</Link>
                </BreadcrumbLink>
              )}
            </BreadcrumbItem>
          </Fragment>
        ))}
      </BreadcrumbList>
    </Breadcrumb>
  );
}
