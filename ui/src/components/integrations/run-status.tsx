import { cn } from "cn";
import { CircleCheck, CircleX, Loader2, TriangleAlert } from "lucide-react";
import type { Run } from "@/features/integrations";

// runOutcome sums up a run: it's still running, failed to fetch, wrote everything, or skipped some
// items or warned of something else. Running looks as a running action run does.
export function runOutcome(run: Run) {
  if (!run.finishedAt) {
    return { label: "Running", icon: Loader2, className: "animate-spin text-primary" };
  }
  if (run.error) return { label: "Failed", icon: CircleX, className: "text-destructive" };
  if (run.failed > 0 || run.warning) {
    return { label: "Succeeded with Warnings", icon: TriangleAlert, className: "text-warning" };
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
