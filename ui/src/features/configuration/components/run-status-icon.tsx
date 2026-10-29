import { cn } from "cn";
import {
  CircleAlertIcon,
  CircleCheckIcon,
  CircleXIcon,
  LoaderCircleIcon,
  type LucideIcon,
} from "lucide-react";
import type { RunStatus } from "@/features/integrations";

const statusPresentation: Record<
  RunStatus,
  { Icon: LucideIcon; className: string; label: string }
> = {
  running: {
    Icon: LoaderCircleIcon,
    className: "animate-spin text-muted-foreground",
    label: "Running",
  },
  succeeded: { Icon: CircleCheckIcon, className: "text-success", label: "Succeeded" },
  partial: { Icon: CircleAlertIcon, className: "text-warning", label: "Succeeded with warnings" },
  failed: { Icon: CircleXIcon, className: "text-destructive", label: "Failed" },
};

export function RunStatusIcon({ status }: Readonly<{ status: RunStatus }>) {
  const { Icon, className, label } = statusPresentation[status];
  return (
    <span role="img" aria-label={label} title={label} className="inline-flex shrink-0">
      <Icon aria-hidden="true" className={cn("size-4", className)} />
    </span>
  );
}
