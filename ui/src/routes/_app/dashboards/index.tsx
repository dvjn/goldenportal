import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { LayoutDashboard, Plus } from "lucide-react";
import { BlueprintIcon } from "@/components/catalog/blueprint-icon";
import { StatusBadges } from "@/components/config-docs/status-badges";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { type DashboardSummary, dashboards } from "@/features/dashboards";

export const Route = createFileRoute("/_app/dashboards/")({
  loader: ({ context }) => context.queryClient.ensureQueryData(dashboards.queries.list),
  component: DashboardsPage,
});

function DashboardsPage() {
  const { data: list } = useSuspenseQuery(dashboards.queries.list);
  const newButton = (
    <Button asChild>
      <Link to="/dashboards/new">
        <Plus />
        New Dashboard
      </Link>
    </Button>
  );

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Dashboards"
        description="The tabs of widgets on Home."
        actions={newButton}
      />
      {list.length === 0 ? (
        <Card className="py-0">
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <LayoutDashboard />
              </EmptyMedia>
              <EmptyTitle>No dashboards yet</EmptyTitle>
              <EmptyDescription>
                A dashboard is a grid of widgets, shown as a tab on Home, a blueprint's catalog page
                or its entities' pages.
              </EmptyDescription>
            </EmptyHeader>
            <EmptyContent>{newButton}</EmptyContent>
          </Empty>
        </Card>
      ) : (
        <ul className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {list.map((dashboard) => (
            <li key={dashboard.identifier}>
              <DashboardCard dashboard={dashboard} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function DashboardCard({ dashboard }: Readonly<{ dashboard: DashboardSummary }>) {
  return (
    <Link
      to="/dashboards/$identifier"
      params={{ identifier: dashboard.identifier }}
      className="block h-full rounded-xl focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
    >
      <Card className="h-full transition-colors hover:bg-accent/50">
        <CardHeader>
          <div className="flex items-start gap-3">
            <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted">
              <BlueprintIcon icon={dashboard.icon} className="size-4.5" />
            </span>
            <div className="grid min-w-0 gap-1">
              <CardTitle className="truncate">{dashboard.title}</CardTitle>
              <code className="truncate font-mono text-xs text-muted-foreground">
                {dashboard.identifier}
              </code>
              {dashboard.description && (
                <CardDescription className="line-clamp-2">{dashboard.description}</CardDescription>
              )}
              <StatusBadges doc={dashboard} />
            </div>
          </div>
        </CardHeader>
      </Card>
    </Link>
  );
}
