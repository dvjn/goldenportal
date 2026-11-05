import { useQuery } from "@tanstack/react-query";
import { cn } from "cn";
import {
  Children,
  createContext,
  Fragment,
  isValidElement,
  type ReactNode,
  useContext,
} from "react";
import { IconTile, InCrumb } from "@/components/catalog/blueprint-icon";
import { PageIcon, sectionPage } from "@/components/page-icons";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { blueprints } from "@/features/blueprints";

type PageBreadcrumbsProps = {
  // children are the links to the pages above this one, outermost first.
  children?: ReactNode;
  page: string;
};

// CompactBreadcrumbs fits the trails inside it on one line, as the bar does
// once the page header has scrolled away. Pages build their own
// trail, so the bar sets the variant around it rather than on it.
export const CompactBreadcrumbs = createContext(false);

// PageCrumbIcon is the icon the page header leads with, which the trail's
// last crumb shows too, so it needn't be passed twice. A page without one,
// such as an entity, which has no icon of its own, shows none.
export const PageCrumbIcon = createContext<ReactNode>(null);

// LinkIcon is the icon of the page a link opens, read from where it goes: a
// section's, or a blueprint's catalog page's, tinted as the blueprint is.
// Anything else gets the plain tile, so every crumb leads with one.
export function LinkIcon({ link }: Readonly<{ link: ReactNode }>) {
  const props = isValidElement(link)
    ? (link.props as { to?: unknown; params?: { blueprint?: unknown } })
    : {};
  const section = sectionPage(props.to);
  if (section) return <PageIcon page={section} />;
  if (props.to === "/catalog/$blueprint" && typeof props.params?.blueprint === "string") {
    return <BlueprintTile blueprint={props.params.blueprint} />;
  }
  return <IconTile size="lg" />;
}

function BlueprintTile({ blueprint }: Readonly<{ blueprint: string }>) {
  const { data } = useQuery(blueprints.queries.detail(blueprint));
  return <IconTile icon={data?.icon} color={data?.color} size="lg" />;
}

// PageBreadcrumbs is the trail to a page. Each crumb leads with its icon
// tile: a section's, or the page's own. Over the header it's only the pages
// above, since the header right below names the page; a top-level page has
// none. Compact, once that header has scrolled away, it ends in the page
// and stays on one line: the outermost crumbs truncate first, and on narrow
// screens only the page is left.
export function PageBreadcrumbs({ children, page }: PageBreadcrumbsProps) {
  const compact = useContext(CompactBreadcrumbs);
  const icon = useContext(PageCrumbIcon);
  const links = Children.toArray(children);
  if (!compact && links.length === 0) return null;
  return (
    <InCrumb value>
      <Breadcrumb className={cn(compact && "min-w-0")}>
        <BreadcrumbList className={cn(compact && "flex-nowrap")}>
          {links.map((link, index) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: the trail never reorders.
            <Fragment key={index}>
              <BreadcrumbItem
                className={cn(
                  "gap-1.5",
                  compact && "min-w-0 shrink-4 max-sm:hidden",
                  compact && index === 0 && "shrink-8",
                )}
              >
                <LinkIcon link={link} />
                <BreadcrumbLink asChild className="truncate">
                  {link}
                </BreadcrumbLink>
              </BreadcrumbItem>
              {(compact || index < links.length - 1) && (
                <BreadcrumbSeparator className={cn(compact && "max-sm:hidden")} />
              )}
            </Fragment>
          ))}
          {compact && (
            <BreadcrumbItem className="min-w-0 gap-1.5">
              {icon}
              <BreadcrumbPage className="max-w-64 truncate">{page}</BreadcrumbPage>
            </BreadcrumbItem>
          )}
        </BreadcrumbList>
      </Breadcrumb>
    </InCrumb>
  );
}
