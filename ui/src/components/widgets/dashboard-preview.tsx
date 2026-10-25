import { Link } from "@tanstack/react-router";
import type { DashboardSpec } from "@/features/dashboards";
import { WidgetGrid } from "./widget-grid";

// DashboardPreview shows a dashboard as Home will.
export function DashboardPreview({
  spec,
  draft,
}: Readonly<{ spec: DashboardSpec; draft: boolean }>) {
  return (
    <div className="grid gap-4">
      <p className="text-sm text-muted-foreground">
        A tab on{" "}
        <Link
          to="/"
          search={{ d: spec.identifier }}
          className="font-medium text-foreground hover:underline"
        >
          Home
        </Link>
        , order {spec.order}
        {draft && " · showing the draft"}
      </p>
      <WidgetGrid
        widgets={spec.widgets}
        source={{ dashboard: spec.identifier }}
        context={{ draft }}
      />
    </div>
  );
}
