import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { NewDocPage } from "@/components/config-docs/new-doc-page";
import { IntegrationPreview } from "@/components/integrations/integration-preview";
import { integrations } from "@/features/integrations";

// Every expression in a mapping is jq, run against the pushed JSON.
const template = `identifier: example
title: Example
description: Where these entities come from.
connector: push
mappings:
  - blueprint: service
    items: .services[]
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
