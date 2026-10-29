import { HTTPError } from "ky";
import { RefreshCwIcon } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { useSyncIntegration } from "@/features/integrations";

function isConflict(error: unknown) {
  return error instanceof HTTPError && error.response.status === 409;
}

export function SyncButton({
  identifier,
  running,
}: Readonly<{ identifier: string; running: boolean }>) {
  const sync = useSyncIntegration(identifier);
  const busy = running || sync.isPending;

  return (
    <div className="flex flex-col items-end gap-1">
      <Button
        type="button"
        variant="outline"
        size="sm"
        disabled={busy}
        onClick={() => sync.mutate(undefined, { onSuccess: () => toast("Sync started") })}
      >
        {busy ? <Spinner /> : <RefreshCwIcon />}
        Sync now
      </Button>
      {sync.isError ? (
        <span className="text-xs text-destructive">
          {isConflict(sync.error) ? "A sync is already running." : "Could not start the sync."}
        </span>
      ) : null}
    </div>
  );
}
