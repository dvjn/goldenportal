import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { ActionPreview } from "@/components/actions/action-preview";
import { NewDocPage } from "@/components/config-docs/new-doc-page";
import { workflows } from "@/features/actions";

// A workflow isn't tied to a blueprint: group says where it shows in the
// catalog, and an action step runs an action on each entity given.
const template = `kind: Workflow
metadata:
  name: example
  title: Example
  description: What it does, as people will see it.
  icon: sparkles
spec:
  group: Operations
  run:
    roles: [member]
  inputs:
    services:
      title: Services
      relation: service
      many: true
      required: true
    reason:
      title: Reason
      type: string
      required: true
  steps:
    - id: restart
      title: Restart each service
      action:
        name: restart
        entities: $input.services
        inputs:
          reason: $input.reason
        maxFailures: 0
    - output:
        summary: "Restarted {{ $step.restart.succeeded }} services"
`;

export const Route = createFileRoute("/_app/workflows/new")({
  component: NewWorkflowPage,
});

function NewWorkflowPage() {
  const navigate = useNavigate();

  return (
    <NewDocPage
      docs={workflows}
      title="New Workflow"
      parent={<Link to="/workflows">Workflows</Link>}
      template={template}
      onCreated={(identifier) =>
        navigate({
          to: "/workflows/$identifier",
          params: { identifier },
          search: { tab: "definition" },
        })
      }
      preview={(spec) => <ActionPreview spec={spec} />}
    />
  );
}
