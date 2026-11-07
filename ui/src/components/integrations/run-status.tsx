import { CircleCheck, CircleX, Loader2, TriangleAlert } from "lucide-react";
import { RunStatusMark, type StatusLook } from "@/components/run-status-mark";
import type { Run, RunOutcome } from "@/features/integrations";

// Running looks as a running action run does.
export const runOutcomes: Record<RunOutcome, StatusLook> = {
  running: { label: "Running", icon: Loader2, className: "animate-spin text-primary" },
  succeeded: { label: "Succeeded", icon: CircleCheck, className: "text-success" },
  warnings: { label: "Succeeded with Warnings", icon: TriangleAlert, className: "text-warning" },
  failed: { label: "Failed", icon: CircleX, className: "text-destructive" },
};

// runOutcome sums up a run: it's still running, failed to fetch, wrote everything, or skipped some
// items or warned of something else.
export function runOutcome(run: Run): RunOutcome {
  if (!run.finishedAt) return "running";
  if (run.error) return "failed";
  if (run.failed > 0 || run.warning) return "warnings";
  return "succeeded";
}

// RunStatus shows an integration run's outcome as an icon, and its label when asked.
export function RunStatus({ run, label = false }: Readonly<{ run: Run; label?: boolean }>) {
  return <RunStatusMark look={runOutcomes[runOutcome(run)]} label={label} />;
}
