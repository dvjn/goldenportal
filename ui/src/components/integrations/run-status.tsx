import { cn } from "cn";
import { CircleCheck, CircleX, TriangleAlert } from "lucide-react";
import type { Run } from "@/features/integrations";

// runOutcome sums up a run: it failed to fetch, wrote everything, or skipped some items.
export function runOutcome(run: Run) {
  if (run.error) return { label: "Failed", icon: CircleX, className: "text-destructive" };
  if (run.failed > 0) {
    return { label: "Succeeded with warnings", icon: TriangleAlert, className: "text-warning" };
  }
  return { label: "Succeeded", icon: CircleCheck, className: "text-success" };
}

// RunStatus shows a run's outcome as an icon, and its label when asked.
export function RunStatus({ run, label = false }: Readonly<{ run: Run; label?: boolean }>) {
  const outcome = runOutcome(run);
  const Icon = outcome.icon;
  return (
    <span className="inline-flex items-center gap-1.5" title={outcome.label}>
      <Icon className={cn("size-4 shrink-0", outcome.className)} aria-label={outcome.label} />
      {label && <span>{outcome.label}</span>}
    </span>
  );
}
