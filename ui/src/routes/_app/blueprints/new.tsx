import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { BlueprintPreview } from "@/components/blueprints/blueprint-preview";
import { NewDocPage } from "@/components/config-docs/new-doc-page";
import { blueprints } from "@/features/blueprints";

// Every entity has identifier, title, blueprint, integration, createdAt
// and updatedAt, so properties can't take those names. A relation names a
// blueprint, a lookup a plain path through relations, such as owner.title,
// compute jq over the flat entity, such as .url != null, and a scorecard
// property the scorecard whose result it holds.
const template = `kind: Blueprint
metadata:
  identifier: example
  title: Example
  description: What entities of this blueprint are.
spec:
  properties:
    url:
      title: URL
      type: string
      format: uri
    owner:
      title: Owner
      relation: team
    ownerSlack:
      title: Owner Slack
      lookup: owner.slackChannel
`;

export const Route = createFileRoute("/_app/blueprints/new")({
  component: NewBlueprintPage,
});

function NewBlueprintPage() {
  const navigate = useNavigate();

  return (
    <NewDocPage
      docs={blueprints}
      title="New Blueprint"
      parent={<Link to="/blueprints">Blueprints</Link>}
      template={template}
      onCreated={(identifier) =>
        navigate({
          to: "/blueprints/$identifier",
          params: { identifier },
          search: { tab: "definition" },
        })
      }
      preview={(spec) => <BlueprintPreview spec={spec} />}
    />
  );
}
