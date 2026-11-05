import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronDown, Loader2, RefreshCw } from "lucide-react";
import { toast } from "sonner";
import { IconButton } from "@/components/icon-button";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  integrationKeys,
  type RunMode,
  statusQueryOptions,
  syncIntegration,
} from "@/features/integrations";
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

// SyncNow starts syncing an integration that pulls now, on top of its schedule. The
// button syncs as the schedule would next; the menu forces a complete sync,
// which also deletes what neither it nor the complete run before it saw. The
// sync carries on after it answers, so its runs show how it ends.
export function SyncNow({ identifier }: Readonly<{ identifier: string }>) {
  const queryClient = useQueryClient();
  const { data } = useQuery(statusQueryOptions);
  const lastRun = data?.find((s) => s.integration === identifier)?.lastRun;
  const sync = useMutation({
    mutationFn: (mode?: RunMode) => syncIntegration(identifier, mode),
    // Refreshing shows the running run, this one or the one that refused it, and keeps reading
    // until it finishes.
    onSettled: () =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: integrationKeys.runs(identifier) }),
        queryClient.invalidateQueries({ queryKey: integrationKeys.status }),
      ]),
    onSuccess: () => toast.success("Started the sync"),
    onError: (error) => toast.error(error.message),
  });
  // Another sync would be refused while one runs, however it started.
  const running = sync.isPending || (lastRun ? !lastRun.finishedAt : false);

  return (
    <div className="flex items-center gap-1">
      <Button variant="outline" onClick={() => sync.mutate(undefined)} disabled={running}>
        {running ? <Loader2 className="animate-spin" /> : <RefreshCw />}
        Sync Now
      </Button>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <IconButton label="More Sync Options" variant="outline" size="icon" disabled={running}>
            <ChevronDown />
          </IconButton>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem onSelect={() => sync.mutate("complete")}>
            <RefreshCw />
            Complete Sync
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
