import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { House } from "lucide-react";
import { DocPage, validateDocSearch } from "@/components/config-docs/doc-page";
import { PageError } from "@/components/page-error";
import { Button } from "@/components/ui/button";
import { DashboardPreview } from "@/components/widgets/dashboard-preview";
import { dashboards } from "@/features/dashboards";

export const Route = createFileRoute("/_app/dashboards/$identifier")({
  validateSearch: validateDocSearch,
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(dashboards.queries.detail(params.identifier)),
      context.queryClient.ensureQueryData(dashboards.queries.versions(params.identifier)),
    ]),
  component: DashboardPage,
  errorComponent: (props) => (
    <PageError {...props} back={<Link to="/dashboards">Back to Dashboards</Link>} />
  ),
});

function DashboardPage() {
  const { identifier } = Route.useParams();
  const { tab } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { data: dashboard } = useSuspenseQuery(dashboards.queries.detail(identifier));

  return (
    <DocPage
      docs={dashboards}
      doc={dashboard}
      parent={<Link to="/dashboards">Dashboards</Link>}
      noun="Dashboard"
      deleteDescription="This deletes the dashboard, its versions and draft. What it shows isn't touched."
      onDeleted={() => navigate({ to: "/dashboards" })}
      tab={tab}
      onTabChange={(next) => navigate({ search: { tab: next }, replace: true })}
      actions={
        // Home shows only what's published.
        dashboard.published && (
          <Button variant="outline" asChild>
            <Link to="/" search={{ d: identifier }}>
              <House />
              Open on Home
            </Link>
          </Button>
        )
      }
      overview={(spec, doc) => <DashboardPreview spec={spec} draft={!doc.published} />}
    />
  );
}
