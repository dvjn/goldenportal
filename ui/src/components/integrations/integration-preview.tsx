import { useQuery } from "@tanstack/react-query";
import { IconTile } from "@/components/catalog/blueprint-icon";
import { BlueprintLabel } from "@/components/catalog/blueprint-label";
import { connectorsQueryOptions, type IntegrationSpec } from "@/features/integrations";

// IntegrationPreview sums up a new integration while it's being written.
export function IntegrationPreview({ spec }: Readonly<{ spec: IntegrationSpec }>) {
  const { data: connectors } = useQuery(connectorsQueryOptions);
  const connector = connectors?.find((c) => c.id === spec.connector.type);
  // Two mappings can write the same blueprint.
  const writes = [...new Set(spec.mappings.map((mapping) => mapping.blueprint))];
  return (
    <dl className="grid gap-3 text-sm">
      <div className="grid gap-1.5">
        <dt className="text-muted-foreground">Connector</dt>
        <dd className="flex items-center gap-2">
          <IconTile icon={connector?.icon} color={connector?.color} size="sm" />
          {connector?.title ?? spec.connector.type}
        </dd>
      </div>
      <div className="grid gap-1.5">
        <dt className="text-muted-foreground">Writes</dt>
        {writes.map((blueprint) => (
          <dd key={blueprint}>
            <BlueprintLabel blueprint={blueprint} />
          </dd>
        ))}
      </div>
    </dl>
  );
}
