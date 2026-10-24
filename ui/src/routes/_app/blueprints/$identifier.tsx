import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { BlueprintOverview } from "@/components/blueprints/blueprint-overview";
import { DocPage, validateDocSearch } from "@/components/config-docs/doc-page";
import { blueprints, useBlueprint } from "@/features/blueprints";

export const Route = createFileRoute("/_app/blueprints/$identifier")({
  validateSearch: validateDocSearch,
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(blueprints.queries.detail(params.identifier)),
      context.queryClient.ensureQueryData(blueprints.queries.versions(params.identifier)),
    ]),
  component: BlueprintPage,
});

function BlueprintPage() {
  const { identifier } = Route.useParams();
  const { tab } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const blueprint = useBlueprint(identifier);

  return (
    <DocPage
      docs={blueprints}
      doc={blueprint}
      noun="Blueprint"
      deleteDescription="This deletes the blueprint, its versions and draft, and every entity of it. It can't be undone."
      onDeleted={() => navigate({ to: "/blueprints" })}
      tab={tab}
      onTabChange={(next) => navigate({ search: { tab: next }, replace: true })}
      overview={(spec) => <BlueprintOverview spec={spec} />}
    />
  );
}
