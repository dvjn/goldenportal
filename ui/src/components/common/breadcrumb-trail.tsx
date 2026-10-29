import { Link, useMatches } from "@tanstack/react-router";
import {
  createContext,
  Fragment,
  type ReactNode,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { Brand } from "./brand";

export type Ancestor = { label: string; to: string };

declare module "@tanstack/react-router" {
  interface StaticDataRouteOption {
    breadcrumb?: string;
  }
}

type AncestorsContextValue = {
  ancestors: Ancestor[];
  setAncestors: (ancestors: Ancestor[]) => void;
};

const AncestorsContext = createContext<AncestorsContextValue | null>(null);

export function BreadcrumbTrailProvider({ children }: { children: ReactNode }) {
  const [ancestors, setAncestors] = useState<Ancestor[]>([]);
  const value = useMemo(() => ({ ancestors, setAncestors }), [ancestors]);
  return <AncestorsContext.Provider value={value}>{children}</AncestorsContext.Provider>;
}

export function useBreadcrumbAncestors(ancestors: Ancestor[]) {
  const setAncestors = useContext(AncestorsContext)?.setAncestors;
  const serialized = JSON.stringify(ancestors);

  useEffect(() => {
    setAncestors?.(JSON.parse(serialized) as Ancestor[]);
    return () => setAncestors?.([]);
  }, [serialized, setAncestors]);
}

function withoutTrailingSlash(pathname: string) {
  return pathname.replace(/\/$/, "");
}

function useRouteAncestors(): Ancestor[] {
  return useMatches({
    select: (matches) => {
      const currentPath = withoutTrailingSlash(matches[matches.length - 1]?.pathname ?? "");
      return matches.flatMap(({ pathname, staticData }) =>
        staticData.breadcrumb && withoutTrailingSlash(pathname) !== currentPath
          ? [{ label: staticData.breadcrumb, to: pathname }]
          : [],
      );
    },
  });
}

export function HeaderBreadcrumb() {
  const routeAncestors = useRouteAncestors();
  const pageAncestors = useContext(AncestorsContext)?.ancestors ?? [];
  const ancestors = [...routeAncestors, ...pageAncestors];

  return (
    <Breadcrumb className="min-w-0">
      <BreadcrumbList className="flex-nowrap gap-3 text-lg">
        <BreadcrumbItem className="shrink-0">
          <Link
            to="/"
            className="flex items-center rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
            aria-label="Go to home"
          >
            <Brand compact={ancestors.length > 0} />
          </Link>
        </BreadcrumbItem>
        {ancestors.map((ancestor) => (
          <Fragment key={`${ancestor.to}:${ancestor.label}`}>
            <BreadcrumbSeparator className="hidden sm:block">/</BreadcrumbSeparator>
            <BreadcrumbItem className="hidden min-w-0 sm:inline-flex">
              <BreadcrumbLink asChild>
                <Link to={ancestor.to} className="truncate">
                  {ancestor.label}
                </Link>
              </BreadcrumbLink>
            </BreadcrumbItem>
          </Fragment>
        ))}
      </BreadcrumbList>
    </Breadcrumb>
  );
}
