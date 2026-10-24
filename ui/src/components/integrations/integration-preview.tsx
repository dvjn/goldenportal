import type { IntegrationSpec } from "@/features/integrations";

// IntegrationPreview sums up a new integration while it's being written.
export function IntegrationPreview({ spec }: Readonly<{ spec: IntegrationSpec }>) {
  return (
    <dl className="grid gap-3 text-sm">
      <div>
        <dt className="text-muted-foreground">Connector</dt>
        <dd className="font-mono">{spec.connector}</dd>
      </div>
      <div>
        <dt className="text-muted-foreground">Writes</dt>
        <dd>{spec.mappings.map((mapping) => mapping.blueprint).join(", ")}</dd>
      </div>
    </dl>
  );
}
