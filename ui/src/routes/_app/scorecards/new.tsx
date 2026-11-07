import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { NewDocPage } from "@/components/config-docs/new-doc-page";
import { TierBadge } from "@/components/scorecards/tier-badge";
import { requireRole } from "@/features/auth";
import { type ScorecardSpec, scorecards } from "@/features/scorecards";

// Checks are jq over the flat entity: .owner, not .properties.owner. A
// blueprint shows the results once it declares a property such as
// readiness: {type: scorecard, scorecard: example}.
const template = `kind: Scorecard
metadata:
  identifier: example
  title: Example
  description: What good looks like.
spec:
  blueprint: service
  tiers:
    - name: bronze
      title: Bronze
      criteria:
        hasOwner:
          title: Has an owner
          check: .owner != null
          fix: Set the owner where this entity comes from.
`;

export const Route = createFileRoute("/_app/scorecards/new")({
  beforeLoad: ({ context }) => requireRole(context.queryClient, "builder"),
  component: NewScorecardPage,
});

function NewScorecardPage() {
  const navigate = useNavigate();

  return (
    <NewDocPage
      docs={scorecards}
      title="New Scorecard"
      parent={<Link to="/scorecards">Scorecards</Link>}
      template={template}
      onCreated={(identifier) =>
        navigate({
          to: "/scorecards/$identifier",
          params: { identifier },
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
