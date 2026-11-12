import { CircleCheck, CircleMinus, CircleX, TriangleAlert } from "lucide-react";
import type { Status } from "@/features/scorecards";

// checkStatuses are how a check's outcome looks, in the order results list them.
export const checkStatuses: Record<
  Status,
  { icon: typeof CircleCheck; className: string; label: string }
> = {
  passed: {
    icon: CircleCheck,
    className: "text-emerald-600 dark:text-emerald-400",
    label: "Passed",
  },
  failed: { icon: CircleX, className: "text-destructive", label: "Failed" },
  skipped: { icon: CircleMinus, className: "text-muted-foreground", label: "Skipped" },
  errored: {
    icon: TriangleAlert,
    className: "text-amber-600 dark:text-amber-400",
    label: "Errored",
  },
};
