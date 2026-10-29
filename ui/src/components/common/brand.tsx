import { useConfig } from "@/features/config";
import { SourceIcon } from "./source-icon";

export function Brand({ compact = false }: { compact?: boolean }) {
  const portalName = useConfig().name;

  return (
    <span className="flex items-center gap-3">
      <SourceIcon icon="portal" className="size-8" />
      <span
        className="text-lg font-bold tracking-tight text-foreground data-[compact=true]:hidden sm:data-[compact=true]:inline"
        data-compact={compact}
      >
        {portalName}
      </span>
    </span>
  );
}
