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

// A route whose crumb names what it shows, such as an entity's title,
// returns the crumb from its loader.
type CrumbLoaderData = { breadcrumb?: string } | undefined;

const home: Crumb = { path: "/", label: "Home" };

export function PageBreadcrumbs() {
  const crumbs = useMatches({
    select: (matches) =>
      matches.flatMap(({ pathname, staticData, loaderData }) => {
        const label = (loaderData as CrumbLoaderData)?.breadcrumb ?? staticData.breadcrumb;
        return label ? [{ path: pathname, label }] : [];
      }),
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
