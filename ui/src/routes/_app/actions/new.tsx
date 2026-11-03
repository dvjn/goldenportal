import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { ActionPreview } from "@/components/actions/action-preview";
import { NewDocPage } from "@/components/config-docs/new-doc-page";
import { actions } from "@/features/actions";

// Inputs are written as blueprint properties are. Steps run in order, and
// $entity, $input, $me and $step refer to what a run has so far.
const template = `kind: Action
metadata:
  name: example
  title: Example
  description: What it does, as people will see it.
  icon: zap
spec:
  blueprint: service
  run:
    roles: [member]
  inputs:
    reason:
      title: Reason
      type: string
      required: true
  steps:
    - id: call
      title: Call the service
      http:
        method: POST
        url: https://example.com/services/{{ $entity }}/hooks
        body:
          reason: $input.reason
        output:
          id: .body.id
    - output:
        summary: "Done on {{ $entity.title }}"
`;

export const Route = createFileRoute("/_app/actions/new")({
  component: NewActionPage,
});

function NewActionPage() {
  const navigate = useNavigate();

  return (
    <NewDocPage
      docs={actions}
      title="New Action"
      parent={<Link to="/actions">Actions</Link>}
      template={template}
      onCreated={(identifier) =>
        navigate({
          to: "/actions/$identifier",
          params: { identifier },
          search: { tab: "definition" },
        })
      }
      preview={(spec) => <ActionPreview spec={spec} />}
    />
  );
}
