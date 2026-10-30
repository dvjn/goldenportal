import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { DocPage, validateDocSearch } from "@/components/config-docs/doc-page";
import { IntegrationOverview } from "@/components/integrations/integration-overview";
import { integrations, useIntegration } from "@/features/integrations";

export const Route = createFileRoute("/_app/integrations/$identifier")({
  validateSearch: validateDocSearch,
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(integrations.queries.detail(params.identifier)),
      context.queryClient.ensureQueryData(integrations.queries.versions(params.identifier)),
    ]),
  component: IntegrationPage,
});

function IntegrationPage() {
  const { identifier } = Route.useParams();
  const { tab } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const integration = useIntegration(identifier);

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
      overview={(spec) => <IntegrationOverview spec={spec} />}
    />
  );
}
