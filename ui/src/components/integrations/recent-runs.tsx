import { useSuspenseQuery } from "@tanstack/react-query";
import { CircleAlert } from "lucide-react";
import { useState } from "react";
import { IconButton } from "@/components/icon-button";
import { RunStatus } from "@/components/integrations/run-status";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { type Run, runsQueryOptions } from "@/features/integrations";
import { formatAbsolute, formatDuration, formatRelative } from "@/lib/format";

// runsShown is how many runs show before "Show older runs".
const runsShown = 10;

const counts = [
  { key: "received", label: "Received" },
  { key: "created", label: "Created" },
  { key: "updated", label: "Updated" },
  { key: "unchanged", label: "Unchanged" },
  { key: "deleted", label: "Deleted" },
  { key: "failed", label: "Failed" },
] as const;

// triggerLabel says what started a run: a push names its token.
function triggerLabel(run: Run) {
  if (run.trigger === "schedule") return "Schedule";
  if (run.trigger === "manual") return "Sync Now";
  return run.token ? `Push · ${run.token}` : "Push";
}

// RecentRuns lists the latest runs and what each one did.
export function RecentRuns({ identifier }: Readonly<{ identifier: string }>) {
  const { data: runs } = useSuspenseQuery(runsQueryOptions(identifier));
  const [expanded, setExpanded] = useState(false);
  const shown = expanded ? runs : runs.slice(0, runsShown);

  if (runs.length === 0) {
    return <p className="p-4 text-sm text-muted-foreground">Nothing has run yet.</p>;
  }

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-0 pl-4">
              <span className="sr-only">Status</span>
            </TableHead>
            <TableHead>When</TableHead>
            <TableHead>Trigger</TableHead>
            <TableHead className="text-right">Took</TableHead>
            {counts.map((count) => (
              <TableHead key={count.key} className="text-right">
                {count.label}
              </TableHead>
            ))}
            <TableHead className="pr-4" />
          </TableRow>
        </TableHeader>
        <TableBody>
          {shown.map((run) => (
            <TableRow key={run.id}>
              <TableCell className="pl-4">
                <RunStatus run={run} />
              </TableCell>
              <TableCell title={formatAbsolute(run.startedAt)}>
                {formatRelative(run.startedAt)}
              </TableCell>
              <TableCell
                className={run.error ? "font-medium text-destructive" : "text-muted-foreground"}
              >
                {triggerLabel(run)}
              </TableCell>
              <TableCell className="text-right text-muted-foreground tabular-nums">
                {formatDuration(run.startedAt, run.finishedAt)}
              </TableCell>
              {counts.map((count) => (
                <TableCell
                  key={count.key}
                  className={
                    count.key === "failed" && run.failed > 0
                      ? "text-right font-medium text-destructive tabular-nums"
                      : "text-right text-muted-foreground tabular-nums"
                  }
                >
                  {run[count.key]}
                </TableCell>
              ))}
              <TableCell className="pr-4 text-right">
                {(run.error || run.errors.length > 0) && <RunErrors run={run} />}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      {runs.length > shown.length && (
        <div className="border-t p-2 text-center">
          <Button variant="ghost" size="sm" onClick={() => setExpanded(true)}>
            Show {runs.length - shown.length} older runs
          </Button>
        </div>
      )}
    </>
  );
}

function RunErrors({ run }: Readonly<{ run: Run }>) {
  return (
    <Dialog>
      <DialogTrigger asChild>
        <IconButton label="View failures">
          <CircleAlert />
        </IconButton>
      </DialogTrigger>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Failures</DialogTitle>
          <DialogDescription>
            {run.error
              ? "The sync fetched nothing, so its entities were left as they were."
              : `${run.failed} of ${run.received} items weren't written` +
                (run.failed > run.errors.length
                  ? `; the first ${run.errors.length} are shown`
                  : "") +
                "."}
          </DialogDescription>
        </DialogHeader>
        {run.error && <p className="font-mono text-sm break-words">{run.error}</p>}
        <ul className="grid max-h-[60vh] gap-3 overflow-y-auto">
          {run.errors.map((error) => (
            <li key={error.index} className="grid gap-1 text-sm">
              <span className="font-medium">
                Item {error.index + 1}
                {error.blueprint && (
                  <code className="ml-2 font-mono text-xs text-muted-foreground">
                    {error.blueprint}/{error.identifier || "?"}
                  </code>
                )}
              </span>
              <ul className="list-disc pl-5 text-muted-foreground">
                {error.problems.map((problem) => (
                  <li key={problem}>{problem}</li>
                ))}
              </ul>
            </li>
          ))}
        </ul>
      </DialogContent>
    </Dialog>
  );
}
