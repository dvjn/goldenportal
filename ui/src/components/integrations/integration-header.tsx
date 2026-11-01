import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, RefreshCw } from "lucide-react";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { integrationKeys, statusQueryOptions, syncIntegration } from "@/features/integrations";
import { formatAbsolute, formatRelative } from "@/lib/format";
import { RunStatus } from "./run-status";

// LastRun says how an integration last ran and when, beside its title, so
// a failing one shows without opening its runs.
export function LastRun({ identifier }: Readonly<{ identifier: string }>) {
  const { data } = useQuery(statusQueryOptions);
  const status = data?.find((s) => s.integration === identifier);
  if (!status) return null;
  const run = status.lastRun;
  if (!run) return <Badge variant="outline">Never run</Badge>;
  return (
    <Badge variant="outline" title={formatAbsolute(run.startedAt)}>
      <RunStatus run={run} />
      {formatRelative(run.startedAt)}
    </Badge>
  );
}

// SyncNow syncs a pull integration now, on top of its schedule.
export function SyncNow({ identifier }: Readonly<{ identifier: string }>) {
  const queryClient = useQueryClient();
  const sync = useMutation({
    mutationFn: () => syncIntegration(identifier),
    // A failed sync is recorded as a run too, so both outcomes refresh the runs.
    onSettled: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: integrationKeys.runs(identifier) }),
        queryClient.invalidateQueries({ queryKey: integrationKeys.status }),
      ]),
    onSuccess: (run) => {
      toast.success(
        `Synced: ${run.created} created, ${run.updated} updated, ${run.deleted} deleted` +
          (run.failed > 0 ? `, ${run.failed} failed` : ""),
      );
    },
    onError: (error) => toast.error(error.message),
  });

  return (
    <Button variant="outline" onClick={() => sync.mutate()} disabled={sync.isPending}>
      {sync.isPending ? <Loader2 className="animate-spin" /> : <RefreshCw />}
      Sync Now
    </Button>
  );
}
