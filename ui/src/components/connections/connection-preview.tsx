import { useQuery } from "@tanstack/react-query";
import { IconTile } from "@/components/catalog/blueprint-icon";
import { type ConnectionSpec, connectorsQueryOptions } from "@/features/connections";

// ConnectionPreview sums up a new connection while it's being written.
export function ConnectionPreview({ spec }: Readonly<{ spec: ConnectionSpec }>) {
  const { data: connectors } = useQuery(connectorsQueryOptions);
  const connector = connectors?.find((c) => c.id === spec.connector.type);
  return (
    <dl className="grid gap-3 text-sm">
      <div className="grid gap-1.5">
        <dt className="text-muted-foreground">Connector</dt>
        <dd className="flex items-center gap-2">
          <IconTile icon={connector?.icon} color={connector?.color} size="sm" />
          {connector?.title ?? spec.connector.type}
        </dd>
      </div>
    </dl>
  );
}
