import { Link, useRouter } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { isWebUrl } from "@/components/catalog/property-value";
import type { LinkData } from "@/features/widgets";
import { toCatalogLink } from "./catalog-filter";

// portalPath is a path within the portal, or undefined for anything that
// would leave it, such as //host.
function portalPath(path: string) {
  if (!path.startsWith("/")) return undefined;
  const url = new URL(path, window.location.origin);
  return url.origin === window.location.origin ? url.pathname + url.search + url.hash : undefined;
}

// linkable reports whether a link goes anywhere: a catalog link whose
// filter matches nothing doesn't, nor an href that's neither a web URL nor
// a path in the portal.
export function linkable(link: LinkData | undefined): link is LinkData {
  if (!link) return false;
  if (link.href !== undefined) return isWebUrl(link.href) || portalPath(link.href) !== undefined;
  return Boolean(link.blueprint) && toCatalogLink(link.filter) !== undefined;
}

// WidgetLink opens where a widget's link goes: the catalog with the link's
// entities, a page in the portal in place, or a web URL in a new tab.
// Without a link that goes anywhere it's just its children.
export function WidgetLink({
  link,
  className,
  children,
}: Readonly<{ link: LinkData | undefined; className?: string; children: ReactNode }>) {
  const router = useRouter();
  if (!linkable(link)) return <>{children}</>;
  if (link.href !== undefined) {
    const page = portalPath(link.href);
    if (!page) {
      return (
        <a href={link.href} target="_blank" rel="noreferrer" className={className}>
          {children}
        </a>
      );
    }
    return (
      <a
        href={page}
        className={className}
        onClick={(event) => {
          if (
            event.button !== 0 ||
            event.metaKey ||
            event.ctrlKey ||
            event.shiftKey ||
            event.altKey
          )
            return;
          event.preventDefault();
          router.history.push(page);
        }}
      >
        {children}
      </a>
    );
  }
  return (
    <Link
      to="/catalog/$blueprint"
      params={{ blueprint: link.blueprint ?? "" }}
      search={{ view: "entities", ...toCatalogLink(link.filter), sort: link.sort }}
      className={className}
    >
      {children}
    </Link>
  );
}
