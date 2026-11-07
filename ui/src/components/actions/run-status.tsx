import {
  CircleCheck,
  CircleDashed,
  CircleQuestionMark,
  CircleSlash,
  CircleX,
  Hourglass,
  Loader2,
  ThumbsDown,
} from "lucide-react";
import { type StatusLook as Look, RunStatusMark } from "@/components/run-status-mark";
import { Badge } from "@/components/ui/badge";
import type { RunStatus as Status, StepStatus as StepState, StepType } from "@/features/actions";

// Runs and their steps share marks, as integration runs do: a check for
// what worked, a cross for what failed. Not started yet is dashed, and a
// question mark is a step whose outcome someone has to find out. An
// hourglass waits on others, approvers or the runs a step started, and the
// step says which; a thumbs down is approvers' rejection, as on their
// decisions.
const pending = { icon: CircleDashed, className: "text-muted-foreground" };
const waiting = { icon: Hourglass, className: "text-primary" };
const working = { icon: Loader2, className: "animate-spin text-primary" };
const worked = { icon: CircleCheck, className: "text-success" };
const broke = { icon: CircleX, className: "text-destructive" };
const unknown = { icon: CircleQuestionMark, className: "text-warning" };

export const runStatuses: Record<Status, Look> = {
  queued: { label: "Queued", ...pending },
  running: { label: "Running", ...working },
  waiting: { label: "Waiting", ...waiting },
  succeeded: { label: "Succeeded", ...worked },
  failed: { label: "Failed", ...broke },
  rejected: { label: "Rejected", icon: ThumbsDown, className: "text-destructive" },
  interrupted: { label: "Interrupted", ...unknown },
  cancelled: { label: "Cancelled", icon: CircleSlash, className: "text-muted-foreground" },
};

export const stepStatuses: Record<StepState, Look> = {
  pending: { label: "Pending", ...pending },
  running: { label: "Running", ...working },
  waiting: { label: "Waiting", ...waiting },
  done: { label: "Done", ...worked },
  failed: { label: "Failed", ...broke },
  interrupted: { label: "Interrupted", ...unknown },
};

// RunStatus shows an action run's status as an icon, and its label when asked.
export function RunStatus({
  status,
  label = false,
}: Readonly<{ status: Status; label?: boolean }>) {
  return <RunStatusMark look={runStatuses[status]} label={label} />;
}

// stepLook is how a step's status shows; a waiting step says what for.
export function stepLook(step: { status: StepState; type: StepType }): Look {
  const look = stepStatuses[step.status];
  if (step.status !== "waiting") return look;
  if (step.type === "approval") return { ...look, label: "Waiting for approval" };
  if (step.type === "action") return { ...look, label: "Waiting for its runs" };
  return look;
}

export function StepStatus({ step }: Readonly<{ step: { status: StepState; type: StepType } }>) {
  return <RunStatusMark look={stepLook(step)} />;
}

// RunStatusBadge is a run's status beside its title.
export function RunStatusBadge({ status }: Readonly<{ status: Status }>) {
  return (
    <Badge variant="outline">
      <RunStatus status={status} label />
    </Badge>
  );
}
