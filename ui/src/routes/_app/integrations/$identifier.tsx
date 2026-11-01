import { useQuery } from "@tanstack/react-query";
import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { DocPage, type DocTab, validateDocSearch } from "@/components/config-docs/doc-page";
import { LastRun, SyncNow } from "@/components/integrations/integration-header";
import { IntegrationOverview } from "@/components/integrations/integration-overview";
import { PushPanel } from "@/components/integrations/push-panel";
import {
  RecentRuns,
  type RunsSearch,
  validateRunsSearch,
} from "@/components/integrations/recent-runs";
import { PageError } from "@/components/page-error";
import { connectorsQueryOptions, integrations, useIntegration } from "@/features/integrations";

function validateIntegrationSearch(search: Record<string, unknown>): RunsSearch & { tab?: DocTab } {
  return { ...validateDocSearch(search, ["runs", "push"]), ...validateRunsSearch(search) };
}

export const Route = createFileRoute("/_app/integrations/$identifier")({
  validateSearch: validateIntegrationSearch,
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(integrations.queries.detail(params.identifier)),
      context.queryClient.ensureQueryData(integrations.queries.versions(params.identifier)),
    ]),
  component: IntegrationPage,
  errorComponent: (props) => (
    <PageError {...props} back={<Link to="/integrations">Back to Integrations</Link>} />
  ),
});

function IntegrationPage() {
  const { identifier } = Route.useParams();
  const { tab, ...runsSearch } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const integration = useIntegration(identifier);
  const { data: connectors } = useQuery(connectorsQueryOptions);
  // The connector decides how data arrives: pulled on a schedule, which
  // Sync Now runs early once published, or pushed to the endpoint.
  const spec = integration.published?.spec ?? integration.draft?.spec;
  const mode = connectors?.find((c) => c.id === spec?.connector.type)?.mode;

  return (
    <DocPage
      docs={integrations}
      doc={integration}
      parent={<Link to="/integrations">Integrations</Link>}
      noun="Integration"
      deleteDescription="This deletes the integration, its versions, draft and tokens, and every entity it wrote. It can't be undone."
      onDeleted={() => navigate({ to: "/integrations" })}
      tab={tab}
      onTabChange={(next) => navigate({ search: { tab: next }, replace: true })}
      badges={integration.published && <LastRun identifier={identifier} />}
      actions={mode === "pull" && integration.published && <SyncNow identifier={identifier} />}
      overview={(spec) => <IntegrationOverview spec={spec} />}
      extraTabs={[
        {
          value: "runs",
          title: "Runs",
          content: <RecentRuns identifier={identifier} search={runsSearch} />,
        },
        ...(mode === "push"
          ? [{ value: "push", title: "Push", content: <PushPanel identifier={identifier} /> }]
          : []),
      ]}
    />
  );
}
