import type { ReactNode } from "react";

type PageHeaderProps = {
  title: string;
  description?: ReactNode;
  // actions sit at the end of the header, e.g. the page's primary button.
  actions?: ReactNode;
  breadcrumbs?: ReactNode;
};

// PageHeader is the title row every page starts with, so titles look the same everywhere.
export function PageHeader({ title, description, actions, breadcrumbs }: PageHeaderProps) {
  const header = (
    <div className="flex flex-wrap items-end justify-between gap-4">
      <div className="grid gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
        {description && <p className="text-sm text-muted-foreground">{description}</p>}
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
