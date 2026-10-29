import { cn } from "cn";
import {
  CloudUploadIcon,
  DatabaseIcon,
  FileCodeIcon,
  type LucideIcon,
  PencilIcon,
  PlugIcon,
  UploadIcon,
  WebhookIcon,
} from "lucide-react";
import { brandLabel, SourceIcon } from "@/components/common/source-icon";
import { BlueprintIcon } from "@/features/catalog";
import { connectorLabels } from "@/features/integrations";

const fallbacks: Record<string, LucideIcon> = {
  database: DatabaseIcon,
  upload: UploadIcon,
  pencil: PencilIcon,
  webhook: WebhookIcon,
  push: CloudUploadIcon,
  static: FileCodeIcon,
  manual: PencilIcon,
};

export function IntegrationIcon({
  icon,
  className,
}: Readonly<{ icon: string; className?: string }>) {
  return (
    <SourceIcon
      icon={icon}
      fallback={fallbacks[icon] ?? PlugIcon}
      className={cn("size-4 text-muted-foreground", className)}
    />
  );
}

export function ConnectorLabel({ connector }: Readonly<{ connector: string }>) {
  return (
    <span className="inline-flex items-center gap-2">
      <IntegrationIcon icon={connector} />
      {brandLabel(connector) ?? connectorLabels[connector] ?? connector}
    </span>
  );
}

export function BlueprintTile({
  icon,
  className,
}: Readonly<{ icon: string | undefined; className?: string }>) {
  return (
    <span
      className={cn(
        "inline-flex size-6 shrink-0 items-center justify-center rounded-md border bg-muted/50",
        className,
      )}
    >
      <BlueprintIcon icon={icon} className="size-3.5 text-muted-foreground" aria-hidden="true" />
    </span>
  );
}
