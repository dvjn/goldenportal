import { useState } from "react";
import { CopyButton } from "@/components/copy-button";
import { Button } from "@/components/ui/button";
import type { IntegrationDetail } from "@/features/integrations";
import { curlSnippet, gitlabSnippet, ingestPath, tokenVariable } from "../push-snippets";

function Snippet({
  title,
  copyLabel,
  text,
}: Readonly<{ title: string; copyLabel: string; text: string }>) {
  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-sm font-medium">{title}</h3>
        <CopyButton value={text} label={copyLabel} />
      </div>
      <pre className="overflow-x-auto rounded-lg border bg-muted/30 p-3 font-mono text-xs leading-5">
        {text}
      </pre>
    </div>
  );
}

export function PushInstructions({ integration }: Readonly<{ integration: IntegrationDetail }>) {
  const [kind, setKind] = useState(integration.mappings[0]?.kind);
  const mapping = integration.mappings.find((candidate) => candidate.kind === kind);
  const origin = window.location.origin;
  if (!mapping) {
    return (
      <p className="text-sm text-muted-foreground">
        Add a mapping to the integration first, so it knows what it receives.
      </p>
    );
  }

  return (
    <div className="space-y-4">
      <p className="text-sm text-muted-foreground">
        CI jobs and scripts send entities to the endpoint of each kind. Authenticate with an ingest
        token in a masked CI/CD variable named{" "}
        <code className="font-mono text-xs">{tokenVariable}</code>, and replace the example values.
        A snapshot also deletes the {mapping.blueprint_title} entities it leaves out.
      </p>
      {integration.mappings.length > 1 ? (
        <fieldset className="flex flex-wrap gap-2" aria-label="Kind">
          {integration.mappings.map((candidate) => (
            <Button
              key={candidate.kind}
              type="button"
              size="sm"
              variant={candidate.kind === mapping.kind ? "default" : "outline"}
              aria-pressed={candidate.kind === mapping.kind}
              onClick={() => setKind(candidate.kind)}
            >
              {candidate.kind}
            </Button>
          ))}
        </fieldset>
      ) : null}
      <p className="text-sm">
        <span className="mr-2 font-mono text-xs text-muted-foreground">POST</span>
        <code className="font-mono text-xs break-all">
          {origin}
          {ingestPath(integration.identifier, mapping.kind)}
        </code>
      </p>
      <Snippet
        title="curl"
        copyLabel="Copy command"
        text={curlSnippet(origin, integration.identifier, mapping)}
      />
      <Snippet
        title="GitLab CI job"
        copyLabel="Copy job"
        text={gitlabSnippet(origin, integration.identifier, mapping)}
      />
    </div>
  );
}
