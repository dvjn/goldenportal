import type { ReactNode } from "react";
import { usePageTitle } from "@/lib/page-title";

type PageHeaderProps = {
  title: string;
  description?: ReactNode;
  // actions sit at the end of the header, e.g. the page's primary button.
  actions?: ReactNode;
  breadcrumbs?: ReactNode;
  // icon leads the title, e.g. a blueprint's icon tile.
  icon?: ReactNode;
};

// PageHeader is the title row every page starts with, so titles look the same everywhere.
export function PageHeader({ title, description, actions, breadcrumbs, icon }: PageHeaderProps) {
  usePageTitle(title);
  const header = (
    <div className="flex flex-wrap items-end justify-between gap-4">
      <div className="flex min-w-0 items-center gap-3">
        {icon}
        <div className="grid min-w-0 gap-1">
          <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
          {description && <p className="text-sm text-muted-foreground">{description}</p>}
        </div>
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  );
  if (!breadcrumbs) {
    return header;
  }
  return (
    <div className="grid gap-3">
      {breadcrumbs}
      {header}
    </div>
  );
}
