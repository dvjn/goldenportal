import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { Library } from "lucide-react";
import { BlueprintOverview } from "@/components/blueprints/blueprint-overview";
import { DocPage, validateDocSearch } from "@/components/config-docs/doc-page";
import { PageError } from "@/components/page-error";
import { Button } from "@/components/ui/button";
import { blueprints, useBlueprint } from "@/features/blueprints";

export const Route = createFileRoute("/_app/blueprints/$identifier")({
  validateSearch: validateDocSearch,
  loader: ({ context, params }) =>
    Promise.all([
      context.queryClient.ensureQueryData(blueprints.queries.detail(params.identifier)),
      context.queryClient.ensureQueryData(blueprints.queries.versions(params.identifier)),
    ]),
  component: BlueprintPage,
  errorComponent: (props) => (
    <PageError {...props} back={<Link to="/blueprints">Back to Blueprints</Link>} />
  ),
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
      parent={<Link to="/blueprints">Blueprints</Link>}
      noun="Blueprint"
      deleteDescription="This deletes the blueprint, its versions and draft, and every entity of it. It can't be undone."
      onDeleted={() => navigate({ to: "/blueprints" })}
      tab={tab}
      onTabChange={(next) => navigate({ search: { tab: next }, replace: true })}
      actions={
        blueprint.published && (
          <Button variant="outline" asChild>
            <Link to="/catalog/$blueprint" params={{ blueprint: identifier }}>
              <Library />
              View in Catalog
            </Link>
          </Button>
        )
      }
      overview={(spec, doc) => <BlueprintOverview spec={spec} blueprint={doc} />}
    />
  );
}
