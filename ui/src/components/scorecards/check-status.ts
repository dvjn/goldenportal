import { CircleCheck, CircleMinus, CircleX } from "lucide-react";
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
};

// statusLook is how a stored status looks; one this version doesn't know,
// kept from before an evaluation, looks neutral, as skipped does.
export const statusLook = (status: string) =>
  checkStatuses[status as Status] ?? checkStatuses.skipped;
