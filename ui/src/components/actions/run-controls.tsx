import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CircleSlash, ClipboardCheck, Loader2, Play, RotateCw } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Field, FieldError, FieldLabel } from "@/components/ui/field";
import { Textarea } from "@/components/ui/textarea";
import {
  availableQueryOptions,
  cancelRun,
  finished,
  type Resolution,
  type Run,
  resolveRun,
  retryRun,
  runKeys,
  selfServiceCatalogQueryOptions,
} from "@/features/actions";
import { hasRole, useMe } from "@/features/auth";
import { RunActionDialog } from "./run-action-dialog";

// RunControls are what can be done about a run: cancel it until it's over,
// retry or resolve it once it stops short, and run it again. Whoever asked
// for a run and builders can change it; running it again is for whoever
// can run the action on the entity now.
export function RunControls({ run }: Readonly<{ run: Run }>) {
  const me = useMe();
  const builder = hasRole(me.role, "builder");
  const mayChange = builder || run.requester.id === me.id;

  return (
    <>
      {mayChange && !finished(run.status) && <CancelRun run={run} />}
      {mayChange && run.status === "interrupted" && <ResolveRun run={run} builder={builder} />}
      {mayChange && (run.status === "failed" || run.status === "interrupted") && (
        <RetryRun run={run} />
      )}
      <RunAgain run={run} />
    </>
  );
}

function useRunChange(run: Run, change: () => Promise<Run>, done: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: change,
    onSuccess: async (changed) => {
      queryClient.setQueryData(runKeys.detail(run.id), changed);
      await queryClient.invalidateQueries({ queryKey: runKeys.all });
      toast.success(done);
    },
    onError: (error) => toast.error(error.message),
  });
}

function CancelRun({ run }: Readonly<{ run: Run }>) {
  const cancel = useRunChange(run, () => cancelRun(run.id), "Cancelled the run");
  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>
        <Button variant="outline" disabled={cancel.isPending}>
          {cancel.isPending ? <Loader2 className="animate-spin" /> : <CircleSlash />}
          Cancel
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Cancel the run?</AlertDialogTitle>
          <AlertDialogDescription>
            Steps that haven't started won't run. A step under way may still take effect.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Keep Running</AlertDialogCancel>
          <AlertDialogAction variant="destructive" onClick={() => cancel.mutate()}>
            Cancel Run
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

function RetryRun({ run }: Readonly<{ run: Run }>) {
  const retry = useRunChange(run, () => retryRun(run.id), "Retrying from the step it stopped at");
  return (
    <Button variant="outline" onClick={() => retry.mutate()} disabled={retry.isPending}>
      {retry.isPending ? <Loader2 className="animate-spin" /> : <RotateCw />}
      Retry
    </Button>
  );
}

// ResolveRun settles an interrupted run once someone has checked whether
// its step took effect: it failed, or, for builders, it worked and the run
// carries on after it.
function ResolveRun({ run, builder }: Readonly<{ run: Run; builder: boolean }>) {
  const [open, setOpen] = useState(false);
  const [comment, setComment] = useState("");
  const step = run.steps?.find((s) => s.status === "interrupted");
  const queryClient = useQueryClient();
  const resolve = useMutation({
    mutationFn: (outcome: Resolution["outcome"]) => resolveRun(run.id, { outcome, comment }),
    onSuccess: async (changed) => {
      queryClient.setQueryData(runKeys.detail(run.id), changed);
      await queryClient.invalidateQueries({ queryKey: runKeys.all });
      setOpen(false);
      toast.success("Resolved the run");
    },
  });
  const empty = comment.trim() === "";

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) resolve.reset();
      }}
    >
      <DialogTrigger asChild>
        <Button>
          <ClipboardCheck />
          Resolve
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Resolve the run</DialogTitle>
          <DialogDescription>
            The portal lost track of {step ? `"${step.title}"` : "a step"} while it ran, so it can't
            tell whether it took effect. Check, then say how it went.
          </DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="resolve-comment">
            What did you check?
            <span className="text-destructive" title="Required" aria-hidden>
              *
            </span>
          </FieldLabel>
          <Textarea
            id="resolve-comment"
            value={comment}
            onChange={(event) => setComment(event.target.value)}
            aria-required
            rows={3}
          />
          <FieldError>{resolve.error?.message}</FieldError>
        </Field>
        <DialogFooter>
          <Button
            variant="destructive"
            disabled={empty || resolve.isPending}
            onClick={() => resolve.mutate("failed")}
          >
            It Failed
          </Button>
          {builder && (
            <Button disabled={empty || resolve.isPending} onClick={() => resolve.mutate("done")}>
              It Took Effect
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// RunAgain opens the form filled in with the run's inputs, while the
// action can still be run on the entity, or the workflow by the viewer.
function RunAgain({ run }: Readonly<{ run: Run }>) {
  const workflow = run.kind === "workflow";
  const [open, setOpen] = useState(false);
  const { data: available } = useQuery({
    ...availableQueryOptions(run.entity.blueprint, run.entity.identifier),
    enabled: !workflow,
  });
  const { data: catalog } = useQuery({ ...selfServiceCatalogQueryOptions, enabled: workflow });
  const offered = catalog?.groups.flatMap((group) =>
    group.items.filter((item) => item.kind === "workflow"),
  );
  const action = (workflow ? offered : available)?.find((a) => a.identifier === run.action);
  if (!action) return null;

  return (
    <>
      <Button variant="outline" onClick={() => setOpen(true)}>
        <Play />
        Run Again
      </Button>
      <RunActionDialog
        action={action}
        kind={run.kind}
        entity={workflow ? undefined : run.entity}
        inputs={run.inputs}
        open={open}
        onOpenChange={setOpen}
      />
    </>
  );
}
