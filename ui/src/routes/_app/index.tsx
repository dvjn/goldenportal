import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { LayoutDashboard, Plus } from "lucide-react";
import { BlueprintIcon } from "@/components/catalog/blueprint-icon";
import { EmptyCard } from "@/components/empty-state";
import { PageHeader } from "@/components/page-header";
import { PageIcon } from "@/components/page-icons";
import { PageTab } from "@/components/page-tabs";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList } from "@/components/ui/tabs";
import { DashboardInputBar, DashboardView, type Picked } from "@/components/widgets/dashboard-view";
import { hasRole, useMe } from "@/features/auth";
import { homeQueryOptions } from "@/features/dashboards";

// HomeSearch is the open dashboard, d, and what its inputs are set to.
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
      <div className="flex flex-col gap-6">
        <PageHeader title="Home" icon={<PageIcon page="home" />} />
        <EmptyCard
          icon={<LayoutDashboard />}
          title="Nothing on Home yet"
          description={
            builder
              ? "Each dashboard shows here as a tab."
              : "Dashboards show here once a builder adds them."
          }
        >
          {builder && (
            <Button asChild>
              <Link to="/dashboards/new">
                <Plus />
                New Dashboard
              </Link>
            </Button>
          )}
        </EmptyCard>
      </div>
    );
  }

  const active =
    views.find((v) => v.identifier === search.d) ?? views.find((v) => v.preferred) ?? views[0];
  const onPick = (patch: Picked) =>
    navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true, resetScroll: false });
  return (
    <Tabs
      value={active.identifier}
      onValueChange={(next) => navigate({ search: { d: next }, replace: true })}
    >
      <PageHeader
        title="Home"
        icon={<PageIcon page="home" />}
        headless
        // The open dashboard's inputs sit with its tabs, so they join the
        // bar together rather than sticking on their own.
        toolbar={
          active.inputs.length > 0
            ? (compact) => (
                <DashboardInputBar
                  view={active}
                  picked={search}
                  onPick={onPick}
                  compact={compact}
                />
              )
            : undefined
        }
        tabs={
          <TabsList variant="line" aria-label="Dashboards">
            {views.map((v) => (
              <PageTab key={v.identifier} value={v.identifier}>
                {v.icon && <BlueprintIcon icon={v.icon} />}
                {v.title}
              </PageTab>
            ))}
          </TabsList>
        }
      />
      {views.map((v) => (
        <TabsContent key={v.identifier} value={v.identifier} className="grid gap-4 pt-2">
          <DashboardView view={v} picked={search} onPick={onPick} withInputs={false} />
        </TabsContent>
      ))}
    </Tabs>
  );
}
