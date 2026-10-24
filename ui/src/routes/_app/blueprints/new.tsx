import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { BlueprintPreview } from "@/components/blueprints/blueprint-preview";
import { NewDocPage } from "@/components/config-docs/new-doc-page";
import { blueprints } from "@/features/blueprints";

const template = `identifier: example
title: Example
description: What entities of this blueprint are.
properties:
  url:
    title: URL
    type: string
    format: url
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
