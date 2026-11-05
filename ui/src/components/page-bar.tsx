import { cn } from "cn";
import { createContext, type ReactNode, useContext, useMemo, useState } from "react";
import { Separator } from "@/components/ui/separator";
import { SidebarTrigger } from "@/components/ui/sidebar";

// pageBarHeight matches the bar's h-12, so a page knows when its header has
// gone up to where the bar shows.
export const pageBarHeight = 48;

type PageBarContextValue = {
  compact: HTMLElement | null;
  setCompact: (element: HTMLElement | null) => void;
  collapsed: boolean;
  setCollapsed: (collapsed: boolean) => void;
};

const PageBarContext = createContext<PageBarContextValue | null>(null);

// PageBarProvider lets the page's header fill the bar once it's scrolled
// past: its trail, title, tabs and actions.
export function PageBarProvider({ children }: Readonly<{ children: ReactNode }>) {
  const [compact, setCompact] = useState<HTMLElement | null>(null);
  const [collapsed, setCollapsed] = useState(false);
  const value = useMemo(
    () => ({ compact, setCompact, collapsed, setCollapsed }),
    [compact, collapsed],
  );
  return <PageBarContext.Provider value={value}>{children}</PageBarContext.Provider>;
}

// usePageBar is the bar a page header fills, or null outside the app shell.
export function usePageBar() {
  return useContext(PageBarContext);
}

// PageMenuTrigger opens the sidebar on phones, where it's off canvas, set
// apart from the trail after it. On wider screens the sidebar's own rail
// and shortcut fold it away and back.
function PageMenuTrigger() {
  if (!usePageBar()) return null;
  return (
    <>
      <SidebarTrigger className="-ml-1.5 md:hidden" />
      {/* A top-level page has no trail, so nothing follows the trigger to set apart. */}
      <Separator
        orientation="vertical"
        className="mr-1 data-vertical:h-4 data-vertical:self-center md:hidden has-[+:empty]:hidden"
      />
    </>
  );
}

// PageTrail is the row a page starts with: its breadcrumbs, after the
// sidebar's trigger on phones. A page with neither, such as a top-level one
// on a wide screen, leaves the row out.
export function PageTrail({
  children,
  className,
}: Readonly<{ children?: ReactNode; className?: string }>) {
  return (
    <div
      className={cn(
        "flex items-center gap-2 md:has-[[data-slot=page-crumbs]:empty]:hidden",
        className,
      )}
    >
      <PageMenuTrigger />
      <div data-slot="page-crumbs" className="min-w-0 flex-1">
        {children}
      </div>
    </div>
  );
}

// PageBar is the page header squeezed into a bar along the top, shown only
// once the header has scrolled away. It takes no room of its own, so the
// page doesn't jump as it comes and goes. It's opaque, as large text
// scrolling under it would read through any blur.
export function PageBar() {
  const bar = usePageBar();
  const collapsed = bar?.collapsed ?? false;
  return (
    <div className="sticky top-0 z-20 h-0">
      <header
        data-slot="page-bar"
        data-collapsed={collapsed || undefined}
        hidden={!collapsed}
        className="border-b bg-background duration-200 animate-in fade-in slide-in-from-top-2 motion-reduce:animate-none"
      >
        <div className="mx-auto flex h-12 w-full max-w-6xl items-center gap-2 px-4 sm:px-6">
          <PageMenuTrigger />
          <div ref={bar?.setCompact} className="flex min-w-0 flex-1 self-stretch" />
        </div>
      </header>
    </div>
  );
}
