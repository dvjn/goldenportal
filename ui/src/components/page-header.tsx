import { cn } from "cn";
import { type ReactNode, type RefObject, useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { pageBarHeight, usePageBar } from "@/components/page-bar";
import { CompactBreadcrumbs, PageBreadcrumbs, PageCrumbIcon } from "@/components/page-breadcrumbs";
import { Separator } from "@/components/ui/separator";
import { usePageTitle } from "@/lib/page-title";

type PageHeaderProps = {
  title: string;
  // heading renders the title in the header when plain text won't do.
  heading?: ReactNode;
  // badges follow the title on its line, on the page rather than a card,
  // so plain ones take the card's look to stand off it.
  badges?: ReactNode;
  description?: ReactNode;
  // actions sit at the end of the header, e.g. the page's primary button.
  actions?: ReactNode;
  // breadcrumbs default to the page's own name, as on a top-level page.
  breadcrumbs?: ReactNode;
  // icon leads the title, e.g. a blueprint's icon tile.
  icon?: ReactNode;
  // tabs is the page's TabsList, so PageHeader must sit inside its Tabs.
  tabs?: ReactNode;
  // toolbar is a row of controls under the tabs, such as a dashboard's
  // inputs, which joins the bar with them. compact says it's the bar's.
  toolbar?: (compact: boolean) => ReactNode;
  // headless leaves the heading out, for a page its tabs lead. The title
  // still names the page in the document title, breadcrumbs and bar.
  headless?: boolean;
};

// PageHeader is the header every page starts with, so titles look the same
// everywhere. Its breadcrumbs go in the page bar, and once it's scrolled
// past, its trail, tabs, toolbar and actions move up into the bar as one row.
export function PageHeader({
  title,
  heading,
  badges,
  description,
  actions,
  breadcrumbs,
  icon,
  tabs,
  toolbar,
  headless,
}: PageHeaderProps) {
  usePageTitle(title);
  const bar = usePageBar();
  const ref = useRef<HTMLDivElement>(null);
  useScrolledPast(ref, bar?.setCollapsed);
  const collapsed = bar?.collapsed ?? false;
  const crumbs = (
    <PageCrumbIcon value={icon}>{breadcrumbs ?? <PageBreadcrumbs page={title} />}</PageCrumbIcon>
  );

  return (
    <>
      {bar?.crumbs && createPortal(crumbs, bar.crumbs)}
      {collapsed &&
        bar?.compact &&
        createPortal(
          <CompactHeader crumbs={crumbs} tabs={tabs} toolbar={toolbar?.(true)} actions={actions} />,
          bar.compact,
        )}
      <div ref={ref} className="grid gap-6">
        {!bar && breadcrumbs && crumbs}
        {headless ? (
          <h1 className="sr-only">{title}</h1>
        ) : (
          <div className="flex flex-wrap items-end justify-between gap-4">
            <div className="flex min-w-0 items-center gap-3">
              {icon}
              <div className="grid min-w-0 gap-1">
                <div className="flex flex-wrap items-center gap-2">
                  <h1 className="text-2xl font-semibold tracking-tight">{heading ?? title}</h1>
                  {badges && (
                    <span data-slot="page-badges" className="contents">
                      {badges}
                    </span>
                  )}
                </div>
                {description && <div className="text-sm text-muted-foreground">{description}</div>}
              </div>
            </div>
            {actions && <div className="flex items-center gap-2">{actions}</div>}
          </div>
        )}
        {tabs && (
          // Kept in place while the bar shows them, so the page doesn't jump.
          // The rule runs the page's width, and the active tab's line sits on it.
          <div
            className={cn(
              "overflow-x-auto border-b [scrollbar-width:none]",
              collapsed && "invisible",
            )}
            aria-hidden={collapsed}
            inert={collapsed}
          >
            {tabs}
          </div>
        )}
        {toolbar && (
          <div className={cn(collapsed && "invisible")} aria-hidden={collapsed} inert={collapsed}>
            {toolbar(false)}
          </div>
        )}
      </div>
    </>
  );
}

// CompactHeader is the page header squeezed into the bar: its trail, which
// still leads back up and ends in the page's icon and title, then its tabs,
// which fill its height so the active line sits on its border and scroll
// sideways when they don't fit, then its toolbar. The trail truncates before
// it can crowd them.
function CompactHeader({
  crumbs,
  tabs,
  toolbar,
  actions,
}: Readonly<
  Pick<PageHeaderProps, "tabs" | "actions"> & { crumbs: ReactNode; toolbar?: ReactNode }
>) {
  return (
    <div className="flex min-w-0 flex-1 items-center gap-3 duration-200 animate-in slide-in-from-top-1 motion-reduce:animate-none">
      <div className="flex max-w-2/5 min-w-0 shrink-0 items-center gap-2">
        <CompactBreadcrumbs value>{crumbs}</CompactBreadcrumbs>
      </div>
      {tabs ? (
        <>
          <Separator
            orientation="vertical"
            className="data-vertical:h-4 data-vertical:self-center"
          />
          <div className="flex min-w-0 flex-1 self-stretch overflow-x-auto [scrollbar-width:none] [&>[data-slot=tabs-list]]:h-full">
            {tabs}
          </div>
        </>
      ) : (
        <div className="flex-1" />
      )}
      {toolbar && <div className="flex shrink-0 items-center gap-2">{toolbar}</div>}
      {actions && <div className="hidden shrink-0 items-center gap-2 sm:flex">{actions}</div>}
    </div>
  );
}

// useScrolledPast tells the bar whether the header has gone up under it.
function useScrolledPast(
  ref: RefObject<HTMLElement | null>,
  onChange: ((past: boolean) => void) | undefined,
) {
  useEffect(() => {
    const element = ref.current;
    if (!element || !onChange || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(
      ([entry]) => {
        onChange(!entry.isIntersecting && entry.boundingClientRect.top < pageBarHeight);
      },
      { rootMargin: `-${pageBarHeight}px 0px 0px 0px` },
    );
    observer.observe(element);
    return () => {
      observer.disconnect();
      onChange(false);
    };
  }, [ref, onChange]);
}
