import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { LayoutDashboard, Plus } from "lucide-react";
import { IconTile } from "@/components/catalog/blueprint-icon";
import { EmptyCard } from "@/components/empty-state";
import { Crumb, PageBreadcrumbs } from "@/components/page-breadcrumbs";
import { PageHeader } from "@/components/page-header";
import { PageIcon } from "@/components/page-icons";
import { Button } from "@/components/ui/button";
import { DashboardInputBar, DashboardView, type Picked } from "@/components/widgets/dashboard-view";
import { hasRole, useMe } from "@/features/auth";
import { homeQueryOptions, openDashboard } from "@/features/dashboards";

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

// HomePage shows one dashboard, which the sidebar lists with the others.
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
          title="No dashboards yet"
          description={
            builder
              ? "Each dashboard shows here, listed under Home in the sidebar."
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

  const active = openDashboard(views, search.d);
  const group = active.group;
  // A group isn't a page of its own, so its crumb opens its first dashboard.
  const first = group && views.find((v) => v.group?.title === group.title);
  const onPick = (patch: Picked) =>
    navigate({ search: (prev) => ({ ...prev, ...patch }), replace: true, resetScroll: false });
  return (
    <>
      <PageHeader
        title={active.title}
        icon={<IconTile icon={active.icon} color={active.color} size="lg" />}
        breadcrumbs={
          <PageBreadcrumbs page={active.title}>
            <Link to="/">Home</Link>
            {group && first && (
              <Crumb icon={<IconTile icon={group.icon} size="lg" />}>
                <Link to="/" search={{ d: first.identifier }}>
                  {group.title}
                </Link>
              </Crumb>
            )}
          </PageBreadcrumbs>
        }
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
      />
      <DashboardView
        key={active.identifier}
        view={active}
        picked={search}
        onPick={onPick}
        withInputs={false}
      />
    </>
  );
}
