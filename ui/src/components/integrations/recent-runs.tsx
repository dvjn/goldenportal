import { useSuspenseQuery } from "@tanstack/react-query";
import { CircleAlert } from "lucide-react";
import { IconButton } from "@/components/icon-button";
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
import { formatAbsolute, formatRelative } from "@/lib/format";

const counts = [
  { key: "received", label: "Received" },
  { key: "created", label: "Created" },
  { key: "updated", label: "Updated" },
  { key: "unchanged", label: "Unchanged" },
  { key: "deleted", label: "Deleted" },
  { key: "failed", label: "Failed" },
] as const;

// RecentRuns lists the latest pushes and what each one did.
export function RecentRuns({ identifier }: Readonly<{ identifier: string }>) {
  const { data: runs } = useSuspenseQuery(runsQueryOptions(identifier));

  if (runs.length === 0) {
    return <p className="p-4 text-sm text-muted-foreground">Nothing has been pushed yet.</p>;
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="pl-4">When</TableHead>
          <TableHead>Token</TableHead>
          {counts.map((count) => (
            <TableHead key={count.key} className="text-right">
              {count.label}
            </TableHead>
          ))}
          <TableHead className="pr-4" />
        </TableRow>
      </TableHeader>
      <TableBody>
        {runs.map((run) => (
          <TableRow key={run.id}>
            <TableCell className="pl-4" title={formatAbsolute(run.startedAt)}>
              {formatRelative(run.startedAt)}
            </TableCell>
            <TableCell className="text-muted-foreground">{run.token || "—"}</TableCell>
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
              {run.errors.length > 0 && <RunErrors run={run} />}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
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
            {run.failed} of {run.received} items weren't written
            {run.failed > run.errors.length && `; the first ${run.errors.length} are shown`}.
          </DialogDescription>
        </DialogHeader>
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
