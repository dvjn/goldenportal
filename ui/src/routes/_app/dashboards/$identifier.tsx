import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { DocPage, validateDocSearch } from "@/components/config-docs/doc-page";
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
      noun="Dashboard"
      deleteDescription="This deletes the dashboard, its versions and draft. What it shows isn't touched."
      onDeleted={() => navigate({ to: "/dashboards" })}
      tab={tab}
      onTabChange={(next) => navigate({ search: { tab: next }, replace: true })}
      overview={(spec, doc) => <DashboardPreview spec={spec} draft={!doc.published} />}
    />
  );
}
