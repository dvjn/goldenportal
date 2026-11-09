import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { NewDocPage } from "@/components/config-docs/new-doc-page";
import { ConnectionPreview } from "@/components/connections/connection-preview";
import { connections } from "@/features/connections";

// A resource's push items are jq over the request; blueprints' sources map
// those items into entities.
const template = `kind: Connection
metadata:
  identifier: example
  title: Example
  description: Where these entities come from.
spec:
  connector:
    type: http
    config:
      resources:
        services:
          push:
            items: .body.services[]
`;

export const Route = createFileRoute("/_app/connections/new")({
  component: NewConnectionPage,
});

function NewConnectionPage() {
  const navigate = useNavigate();

  return (
    <NewDocPage
      docs={connections}
      title="New Connection"
      parent={<Link to="/connections">Connections</Link>}
      template={template}
      onCreated={(identifier) =>
        navigate({
          to: "/connections/$identifier",
          params: { identifier },
          search: { tab: "definition" },
        })
      }
      preview={(spec) => <ConnectionPreview spec={spec} />}
    />
  );
}
