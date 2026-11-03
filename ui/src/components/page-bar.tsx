import { cn } from "cn";
import { createContext, type ReactNode, useContext, useMemo, useState } from "react";
import { Separator } from "@/components/ui/separator";
import { SidebarTrigger } from "@/components/ui/sidebar";

// pageBarHeight matches the bar's h-12, so a page knows when its header has
// gone under the bar.
export const pageBarHeight = 48;

type PageBarContextValue = {
  crumbs: HTMLElement | null;
  compact: HTMLElement | null;
  setCrumbs: (element: HTMLElement | null) => void;
  setCompact: (element: HTMLElement | null) => void;
  collapsed: boolean;
  setCollapsed: (collapsed: boolean) => void;
};

const PageBarContext = createContext<PageBarContextValue | null>(null);

// PageBarProvider lets the page's header fill the bar: its breadcrumbs, and
// once it's scrolled past, its title, tabs and actions.
export function PageBarProvider({ children }: Readonly<{ children: ReactNode }>) {
  const [crumbs, setCrumbs] = useState<HTMLElement | null>(null);
  const [compact, setCompact] = useState<HTMLElement | null>(null);
  const [collapsed, setCollapsed] = useState(false);
  const value = useMemo(
    () => ({ crumbs, compact, setCrumbs, setCompact, collapsed, setCollapsed }),
    [crumbs, compact, collapsed],
  );
  return <PageBarContext.Provider value={value}>{children}</PageBarContext.Provider>;
}

// usePageBar is the bar a page header fills, or null outside the app shell.
export function usePageBar() {
  return useContext(PageBarContext);
}

// PageBar is the sticky bar above every page, after the stock sidebar
// layout: the sidebar trigger, then where you are.
export function PageBar() {
  const bar = usePageBar();
  const collapsed = bar?.collapsed ?? false;
  // Showing a slot again restarts its fade, so each state change eases in.
  const fade = "animate-in duration-200 fade-in motion-reduce:animate-none";
  return (
    <header
      data-slot="page-bar"
      data-collapsed={collapsed || undefined}
      className="sticky top-0 z-20 border-b bg-background/90 backdrop-blur supports-[backdrop-filter]:bg-background/75"
    >
      <div className="mx-auto flex h-12 w-full max-w-6xl items-center gap-2 px-4 sm:px-6">
        <SidebarTrigger className="-ml-1.5" />
        {/* A top-level page has no trail, so nothing follows the trigger to set apart. */}
        <Separator
          orientation="vertical"
          className="mr-1 data-vertical:h-4 data-vertical:self-center [[data-slot=page-bar]:not([data-collapsed]):has([data-slot=page-crumbs]:empty)_&]:hidden"
        />
        <div
          ref={bar?.setCrumbs}
          data-slot="page-crumbs"
          hidden={collapsed}
          className={cn("min-w-0 flex-1", fade)}
        />
        <div
          ref={bar?.setCompact}
          hidden={!collapsed}
          className={cn("flex min-w-0 flex-1 self-stretch", fade)}
        />
      </div>
    </header>
  );
}
