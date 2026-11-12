import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link, redirect, useNavigate } from "@tanstack/react-router";
import { Library } from "lucide-react";
import { DocPage, validateDocSearch } from "@/components/config-docs/doc-page";
import { PageError } from "@/components/page-error";
import { ScorecardOverview } from "@/components/scorecards/scorecard-overview";
import { Button } from "@/components/ui/button";
import { blueprints, useBlueprint } from "@/features/blueprints";
import { builtUnder, scorecards, scorecardView } from "@/features/scorecards";

export const Route = createFileRoute("/_app/blueprints/$blueprint/scorecards/$identifier")({
  validateSearch: validateDocSearch,
  loader: async ({ context, params }) => {
    const [doc] = await Promise.all([
      context.queryClient.ensureQueryData(scorecards.queries.detail(params.identifier)),
      context.queryClient.ensureQueryData(scorecards.queries.versions(params.identifier)),
      context.queryClient.ensureQueryData(blueprints.queries.detail(params.blueprint)),
    ]);
    // A scorecard moved to another blueprint is found under that one.
    const blueprint = builtUnder(doc);
    if (blueprint && blueprint !== params.blueprint) {
      throw redirect({
        to: "/blueprints/$blueprint/scorecards/$identifier",
        params: { blueprint, identifier: params.identifier },
        search: true,
        replace: true,
      });
    }
  },
  component: ScorecardPage,
  errorComponent: (props) => (
    <PageError {...props} back={<Link to="/blueprints">Back to Blueprints</Link>} />
  ),
});

function ScorecardPage() {
  const { blueprint, identifier } = Route.useParams();
  const { tab } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const { data: scorecard } = useSuspenseQuery(scorecards.queries.detail(identifier));
  const parent = useBlueprint(blueprint);

  return (
    <DocPage
      docs={scorecards}
      doc={scorecard}
      parent={[
        <Link key="blueprints" to="/blueprints">
          Blueprints
        </Link>,
        <Link
          key="blueprint"
          to="/blueprints/$identifier"
          params={{ identifier: blueprint }}
          search={{ tab: "scorecards" }}
        >
          {parent.title}
        </Link>,
      ]}
      noun="Scorecard"
      deleteDescription="This deletes the scorecard, its versions and draft, and every result it holds. Entities aren't touched."
      onDeleted={() =>
        navigate({
          to: "/blueprints/$identifier",
          params: { identifier: blueprint },
          search: { tab: "scorecards" },
        })
      }
      tab={tab}
      onTabChange={(next) => navigate({ search: { tab: next }, replace: true })}
      actions={
        scorecard.published && (
          <Button variant="outline" asChild>
            <Link
              to="/catalog/$blueprint"
              params={{ blueprint: scorecard.published.spec.blueprint }}
              search={{ view: scorecardView(identifier) }}
            >
              <Library />
              View in Catalog
            </Link>
          </Button>
        )
      }
      overview={(spec) => (
        <ScorecardOverview spec={spec} published={scorecard.published !== null} />
      )}
    />
  );
}
