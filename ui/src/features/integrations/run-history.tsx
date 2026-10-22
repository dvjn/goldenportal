import { Fragment, useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { sentenceCase } from "@/lib/api";
import { formatDateTime, formatRelative } from "@/lib/time";
import type { Run, RunStatus } from "./api";

const statuses: Record<
  RunStatus,
  { label: string; variant: "default" | "secondary" | "outline" | "destructive" }
> = {
  running: { label: "Running", variant: "default" },
  succeeded: { label: "Succeeded", variant: "secondary" },
  partial: { label: "Partial", variant: "outline" },
  failed: { label: "Failed", variant: "destructive" },
};

export function RunStatusBadge({ status }: { status: RunStatus }) {
  return <Badge variant={statuses[status].variant}>{statuses[status].label}</Badge>;
}

export function runSource(run: Run) {
  switch (run.trigger) {
    case "schedule":
      return "Scheduled";
    case "manual":
      return `Synced by ${run.actor}`;
    case "push":
      return `Pushed with ${run.actor}`;
  }
}

function runWhat(run: Run) {
  return run.trigger === "push" ? [run.kind, run.mode].filter(Boolean).join(", ") : "Full sync";
}

export function runCounts(run: Run) {
  const parts = [
    run.trigger !== "push" && run.counts.fetched > 0 && `${run.counts.fetched} read`,
    run.counts.upserted > 0 && `${run.counts.upserted} written`,
    run.counts.deleted > 0 && `${run.counts.deleted} deleted`,
    run.counts.skipped > 0 && `${run.counts.skipped} skipped`,
  ].filter(Boolean);
  if (run.status === "running") return "In progress";
  return parts.length > 0 ? parts.join(", ") : "Nothing written";
}

export function RunHistory({ runs }: { runs: Run[] }) {
  const [expanded, setExpanded] = useState<Set<number>>(new Set());

  function toggle(id: number) {
    setExpanded((current) => {
      const next = new Set(current);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Status</TableHead>
          <TableHead>Started</TableHead>
          <TableHead>Source</TableHead>
          <TableHead>Run</TableHead>
          <TableHead>Result</TableHead>
          <TableHead>
            <span className="sr-only">Details</span>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {runs.map((run) => {
          const hasDetails = Boolean(run.error) || run.warnings.length > 0;
          const open = expanded.has(run.id);
          return (
            <Fragment key={run.id}>
              <TableRow>
                <TableCell>
                  <RunStatusBadge status={run.status} />
                </TableCell>
                <TableCell title={formatDateTime(run.started_at)}>
                  {formatRelative(run.started_at)}
                </TableCell>
                <TableCell>{runSource(run)}</TableCell>
                <TableCell>{runWhat(run)}</TableCell>
                <TableCell>{runCounts(run)}</TableCell>
                <TableCell className="text-right">
                  {hasDetails && (
                    <Button
                      variant="ghost"
                      size="sm"
                      aria-expanded={open}
                      aria-controls={`run-${run.id}`}
                      onClick={() => toggle(run.id)}
                    >
                      {open ? "Hide details" : detailsLabel(run)}
                    </Button>
                  )}
                </TableCell>
              </TableRow>
              {hasDetails && open && (
                <TableRow id={`run-${run.id}`}>
                  <TableCell colSpan={6} className="whitespace-normal">
                    <div className="flex flex-col gap-2 text-sm">
                      {run.error && <p className="text-destructive">{sentenceCase(run.error)}</p>}
                      {run.warnings.length > 0 && (
                        <ul className="text-muted-foreground flex list-disc flex-col gap-1 pl-5">
                          {run.warnings.map((warning) => (
                            <li key={warning}>{sentenceCase(warning)}</li>
                          ))}
                        </ul>
                      )}
                    </div>
                  </TableCell>
                </TableRow>
              )}
            </Fragment>
          );
        })}
        {runs.length === 0 && (
          <TableRow>
            <TableCell colSpan={6} className="text-muted-foreground text-center">
              No runs yet.
            </TableCell>
          </TableRow>
        )}
      </TableBody>
    </Table>
  );
}

function detailsLabel(run: Run) {
  if (run.error) return "Show error";
  return run.warnings.length === 1 ? "Show 1 warning" : `Show ${run.warnings.length} warnings`;
}
