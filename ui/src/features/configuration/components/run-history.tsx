import { useState } from "react";
import { ListSkeleton } from "@/components/common/list-skeleton";
import { StateCard } from "@/components/common/state-card";
import { TableFrame } from "@/components/common/table-frame";
import { Button } from "@/components/ui/button";
import { TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { type Run, type RunTrigger, useRuns } from "@/features/integrations";
import { sentenceCase } from "@/lib/api";
import { formatDateTime, formatElapsed, formatRelative } from "@/lib/time";
import { RunStatusIcon } from "./run-status-icon";

const recentRunCount = 10;

const columnCount = 8;

const triggerLabels: Record<RunTrigger, string> = {
  schedule: "Schedule",
  manual: "Sync",
  push: "Push",
};

function triggerDetail(run: Run) {
  if (run.trigger === "push") return [run.kind, run.mode].filter(Boolean).join(", ");
  return run.trigger === "manual" ? run.actor : "";
}

function detailsLabel(run: Run) {
  if (run.error) return "Show error";
  return run.warnings.length === 1 ? "Show 1 warning" : `Show ${run.warnings.length} warnings`;
}

function RunRow({ run }: Readonly<{ run: Run }>) {
  const [open, setOpen] = useState(false);
  const hasDetails = Boolean(run.error) || run.warnings.length > 0;
  const finished = run.status !== "running";

  return (
    <>
      <TableRow>
        <TableCell>
          <RunStatusIcon status={run.status} />
        </TableCell>
        <TableCell title={run.trigger === "push" ? `Pushed with ${run.actor}` : undefined}>
          {triggerLabels[run.trigger]}
          {triggerDetail(run) ? (
            <span className="ml-2 text-muted-foreground">{triggerDetail(run)}</span>
          ) : null}
        </TableCell>
        <TableCell className="text-muted-foreground" title={formatDateTime(run.started_at)}>
          {formatRelative(run.started_at)}
        </TableCell>
        <TableCell className="text-muted-foreground tabular-nums">
          {finished ? formatElapsed(run.started_at, run.finished_at) : "In progress"}
        </TableCell>
        <TableCell className="text-right tabular-nums">{run.counts.fetched}</TableCell>
        <TableCell className="text-right tabular-nums">{run.counts.upserted}</TableCell>
        <TableCell className="text-right tabular-nums">{run.counts.deleted}</TableCell>
        <TableCell className="text-right">
          {hasDetails ? (
            <Button
              variant="ghost"
              size="sm"
              aria-expanded={open}
              aria-controls={`run-${run.id}`}
              className={run.error ? "text-destructive" : undefined}
              onClick={() => setOpen(!open)}
            >
              {open ? "Hide details" : detailsLabel(run)}
            </Button>
          ) : null}
        </TableCell>
      </TableRow>
      {hasDetails && open ? (
        <TableRow id={`run-${run.id}`}>
          <TableCell colSpan={columnCount} className="whitespace-normal">
            <div className="flex flex-col gap-2 text-sm">
              {run.error ? <p className="text-destructive">{sentenceCase(run.error)}</p> : null}
              {run.warnings.length > 0 ? (
                <ul className="flex list-disc flex-col gap-1 pl-5 text-muted-foreground">
                  {run.warnings.map((warning) => (
                    <li key={warning}>{sentenceCase(warning)}</li>
                  ))}
                </ul>
              ) : null}
            </div>
          </TableCell>
        </TableRow>
      ) : null}
    </>
  );
}

export function RunHistory({ integration }: Readonly<{ integration: string }>) {
  const runs = useRuns(integration);
  const [showAll, setShowAll] = useState(false);

  if (runs.isError) {
    return <StateCard title="Runs unavailable" message="We could not load the runs." />;
  }
  if (!runs.data) return <ListSkeleton />;
  if (runs.data.length === 0) {
    return <StateCard title="No runs yet" message="This integration has no recorded runs." />;
  }

  const shown = showAll ? runs.data : runs.data.slice(0, recentRunCount);
  const hidden = runs.data.length - shown.length;

  return (
    <TableFrame
      footer={
        hidden > 0 ? (
          <Button variant="ghost" size="sm" onClick={() => setShowAll(true)}>
            Show {hidden} older {hidden === 1 ? "run" : "runs"}
          </Button>
        ) : null
      }
    >
      <TableHeader>
        <TableRow>
          <TableHead>Status</TableHead>
          <TableHead>Trigger</TableHead>
          <TableHead>Started</TableHead>
          <TableHead>Duration</TableHead>
          <TableHead className="text-right">Read</TableHead>
          <TableHead className="text-right">Upserted</TableHead>
          <TableHead className="text-right">Deleted</TableHead>
          <TableHead>
            <span className="sr-only">Details</span>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {shown.map((run) => (
          <RunRow key={run.id} run={run} />
        ))}
      </TableBody>
    </TableFrame>
  );
}
