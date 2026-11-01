import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { LayoutDashboard, Plus } from "lucide-react";
import { EmptyCard } from "@/components/empty-state";
import { LinkCard } from "@/components/link-card";
import { PageHeader } from "@/components/page-header";
import { Button } from "@/components/ui/button";
import { dashboards } from "@/features/dashboards";

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
      <PageHeader title="Dashboards" actions={newButton} />
      {list.length === 0 ? (
        <EmptyCard
          icon={<LayoutDashboard />}
          title="No dashboards yet"
          description="A dashboard is a grid of widgets, shown as a tab on Home, a blueprint's catalog page or its entities' pages."
        >
          {newButton}
        </EmptyCard>
      ) : (
        <ul className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {list.map((dashboard) => (
            <li key={dashboard.identifier}>
              <LinkCard
                to="/dashboards/$identifier"
                params={{ identifier: dashboard.identifier }}
                icon={dashboard.icon}
                color={dashboard.color}
                title={dashboard.title}
                description={dashboard.description}
              />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
