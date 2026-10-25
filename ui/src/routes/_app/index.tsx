import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { LayoutDashboard, Plus } from "lucide-react";
import { BlueprintIcon } from "@/components/catalog/blueprint-icon";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { DashboardView, type Picked } from "@/components/widgets/dashboard-view";
import { hasRole, useMe } from "@/features/auth";
import { homeQueryOptions } from "@/features/dashboards";

// HomeSearch is the open dashboard, d, and what its filters are set to.
type HomeSearch = Picked & { d?: string };

function validateHomeSearch(search: Record<string, unknown>): HomeSearch {
  const out: HomeSearch = {};
  for (const [key, raw] of Object.entries(search)) {
    if (key === "d") {
      if (typeof raw === "string" && raw !== "") out.d = raw;
    } else if (Array.isArray(raw)) {
      out[key] = raw.filter(scalar).map(String);
    } else if (scalar(raw)) {
      out[key] = String(raw);
    }
  }
  return out;
}

function scalar(value: unknown): value is string | number | boolean {
  return typeof value === "string" || typeof value === "number" || typeof value === "boolean";
}

export const Route = createFileRoute("/_app/")({
  validateSearch: validateHomeSearch,
  loader: ({ context }) => context.queryClient.ensureQueryData(homeQueryOptions),
  component: HomePage,
});

// HomePage shows the dashboards, one tab each.
function HomePage() {
  const search = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { data: views } = useSuspenseQuery(homeQueryOptions);
  const builder = hasRole(useMe().role, "builder");

  if (views.length === 0) {
    return (
      <Card className="py-0">
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <LayoutDashboard />
            </EmptyMedia>
            <EmptyTitle>Nothing on Home yet</EmptyTitle>
            <EmptyDescription>
              {builder
                ? "Each dashboard shows here as a tab."
                : "Dashboards show here once a builder adds them."}
            </EmptyDescription>
          </EmptyHeader>
          {builder && (
            <EmptyContent>
              <Button asChild>
                <Link to="/dashboards/new">
                  <Plus />
                  New Dashboard
                </Link>
              </Button>
            </EmptyContent>
          )}
        </Empty>
      </Card>
    );
  }

  const active = views.find((v) => v.identifier === search.d) ?? views[0];
  return (
    <Tabs
      value={active.identifier}
      onValueChange={(next) => navigate({ search: { d: next }, replace: true })}
    >
      <TabsList variant="line" aria-label="Dashboards">
        {views.map((v) => (
          <TabsTrigger
            key={v.identifier}
            value={v.identifier}
            className="focus-visible:border-transparent focus-visible:bg-muted focus-visible:ring-0 focus-visible:outline-none"
          >
            {v.icon && <BlueprintIcon icon={v.icon} />}
            {v.title}
          </TabsTrigger>
        ))}
      </TabsList>
      {views.map((v) => (
        <TabsContent key={v.identifier} value={v.identifier} className="grid gap-4 pt-2">
          {v.description && <p className="text-sm text-muted-foreground">{v.description}</p>}
          <DashboardView
            view={v}
            picked={search}
            onPick={(patch) =>
              navigate({
                search: (prev) => ({ ...prev, ...patch }),
                replace: true,
                resetScroll: false,
              })
            }
          />
        </TabsContent>
      ))}
    </Tabs>
  );
}
