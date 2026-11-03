import { useQuery } from "@tanstack/react-query";
import { CircleAlert } from "lucide-react";
import { useState } from "react";
import { EmptyCard } from "@/components/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { type DashboardSpec, dashboardViewQueryOptions } from "@/features/dashboards";
import { describeError } from "@/lib/api";
import { DashboardView, type Picked } from "./dashboard-view";

// DashboardPreview shows a dashboard as Home will, to the builder: the
// server fills in their $me variables and finds their inputs' defaults,
// from the draft when that's what's shown. What they pick in the input
// bar stays with the preview, not in the URL.
export function DashboardPreview({
  spec,
  draft,
}: Readonly<{ spec: DashboardSpec; draft: boolean }>) {
  const { data: view, error } = useQuery(dashboardViewQueryOptions(spec.identifier, draft));
  const [picked, setPicked] = useState<Picked>({});
  return (
    <div className="grid gap-4">
      {error ? (
        <EmptyCard
          icon={<CircleAlert />}
          title={describeError(error).title}
          description={describeError(error).detail}
        />
      ) : view ? (
        <DashboardView
          view={view}
          picked={picked}
          onPick={(patch) => setPicked((prev) => ({ ...prev, ...patch }))}
          draft={draft}
        />
      ) : (
        <Skeleton className="h-16" aria-hidden />
      )}
    </div>
  );
}
