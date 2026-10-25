import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { type DashboardSpec, dashboardViewQueryOptions } from "@/features/dashboards";
import { DashboardView, type Picked } from "./dashboard-view";

// DashboardPreview shows a dashboard as Home will, to the builder: the
// server fills in their $me variables and finds their filters' defaults,
// from the draft when that's what's shown. What they pick in the filter
// bar stays with the preview, not in the URL.
export function DashboardPreview({
  spec,
  draft,
}: Readonly<{ spec: DashboardSpec; draft: boolean }>) {
  const { data: view, error } = useQuery(dashboardViewQueryOptions(spec.identifier, draft));
  const [picked, setPicked] = useState<Picked>({});
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
      {error ? (
        <p className="text-sm text-muted-foreground">{error.message}</p>
      ) : view ? (
        <DashboardView
          view={view}
          picked={picked}
          onPick={(patch) => setPicked((prev) => ({ ...prev, ...patch }))}
          draft={draft}
        />
      ) : (
        <div className="h-16 animate-pulse rounded-md bg-muted" aria-hidden />
      )}
    </div>
  );
}
