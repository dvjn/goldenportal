import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { NewDocPage } from "@/components/config-docs/new-doc-page";
import { IntegrationPreview } from "@/components/integrations/integration-preview";
import { integrations } from "@/features/integrations";

// A resource's push items are jq over the request; every expression in a
// mapping is jq over one of those items.
const template = `kind: Integration
metadata:
  name: example
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
  mappings:
    - resource: services
      blueprint: service
      entity:
        identifier: .id
        title: .name
        properties:
          owner: .team
`;

export const Route = createFileRoute("/_app/integrations/new")({
  component: NewIntegrationPage,
});

function NewIntegrationPage() {
  const navigate = useNavigate();

  return (
    <NewDocPage
      docs={integrations}
      title="New Integration"
      parent={<Link to="/integrations">Integrations</Link>}
      template={template}
      onCreated={(identifier) =>
        navigate({
          to: "/integrations/$identifier",
          params: { identifier },
          search: { tab: "definition" },
        })
      }
      preview={(spec) => <IntegrationPreview spec={spec} />}
    />
  );
}
