import { useQuery } from "@tanstack/react-query";
import { EyeOff } from "lucide-react";
import { useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { type DashboardSpec, dashboardViewQueryOptions } from "@/features/dashboards";
import { DashboardView, type Picked } from "./dashboard-view";

// Visibility names who can see a dashboard, when it's not everyone.
function Visibility({ visibility }: Readonly<{ visibility: DashboardSpec["visibility"] }>) {
  const who = [...(visibility?.roles ?? []), ...(visibility?.groups ?? [])];
  if (who.length === 0) return null;
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <EyeOff className="size-4 text-muted-foreground" aria-label="Only visible to" />
      {who.map((name) => (
        <Badge key={name} variant="outline" className="font-normal">
          {name}
        </Badge>
      ))}
    </div>
  );
}

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
      <Visibility visibility={spec.visibility} />
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
        <Skeleton className="h-16" aria-hidden />
      )}
    </div>
  );
}
