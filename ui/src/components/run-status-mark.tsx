import { cn } from "cn";
import type { LucideIcon } from "lucide-react";

export type StatusLook = { label: string; icon: LucideIcon; className: string };

// RunStatusMark is the one way a run's status shows: an icon, and its label
// when asked. Each kind of run brings its own map of statuses to looks.
export function RunStatusMark({
  look,
  label = false,
}: Readonly<{ look: StatusLook; label?: boolean }>) {
  const Icon = look.icon;
  return (
    <span className="inline-flex items-center gap-1.5" title={look.label}>
      <Icon className={cn("size-4 shrink-0", look.className)} aria-label={look.label} />
      {label && <span>{look.label}</span>}
    </span>
  );
}
