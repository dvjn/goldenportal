import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { NewDocPage } from "@/components/config-docs/new-doc-page";
import { TierBadge } from "@/components/scorecards/tier-badge";
import { blueprints, useBlueprint } from "@/features/blueprints";
import { type ScorecardSpec, scorecards } from "@/features/scorecards";

// Checks are filters, as the catalog writes them; updatedAt is on every
// blueprint, so the template saves as it is. A blueprint holds the results
// as a property once it declares one such as
// readiness: {type: scorecard, scorecard: example}.
const template = (blueprint: string) => `kind: Scorecard
metadata:
  identifier: example
  title: Example
  description: What good looks like.
spec:
  blueprint: ${blueprint}
  tiers:
    - name: bronze
      title: Bronze
      criteria:
        recentlyUpdated:
          title: Updated in the last 30 days
          check: {property: updatedAt, op: within, value: 30d}
          fix: Check that its source still sends it.
`;

export const Route = createFileRoute("/_app/blueprints/$blueprint/scorecards/new")({
  loader: ({ context, params }) =>
    context.queryClient.ensureQueryData(blueprints.queries.detail(params.blueprint)),
  component: NewScorecardPage,
});

function NewScorecardPage() {
  const { blueprint } = Route.useParams();
  const parent = useBlueprint(blueprint);
  const navigate = useNavigate();

  return (
    <NewDocPage
      docs={scorecards}
      title="New Scorecard"
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
      template={template(blueprint)}
      onCreated={(identifier) =>
        navigate({
          to: "/blueprints/$blueprint/scorecards/$identifier",
          params: { blueprint, identifier },
          search: { tab: "definition" },
        })
      }
      preview={(spec) => <Preview spec={spec} />}
    />
  );
}

function Preview({ spec }: Readonly<{ spec: ScorecardSpec }>) {
  return (
    <div className="grid gap-3 text-sm">
      <p className="text-muted-foreground">
        Scores <code className="font-mono">{spec.blueprint}</code>
      </p>
      {spec.tiers.map((tier) => (
        <div key={tier.name} className="grid gap-1">
          <TierBadge tiers={spec.tiers} name={tier.name} />
          <ul className="grid gap-0.5 pl-3.5 text-muted-foreground">
            {tier.criteria.map((criterion) => (
              <li key={criterion.identifier}>{criterion.title}</li>
            ))}
          </ul>
        </div>
      ))}
    </div>
  );
}
