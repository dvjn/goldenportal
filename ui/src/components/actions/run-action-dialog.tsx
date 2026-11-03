import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Loader2, Play } from "lucide-react";
import { type FormEvent, useState } from "react";
import { IconTile } from "@/components/catalog/blueprint-icon";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { type Available, type RunKind, runKeys, startRun, startWorkflow } from "@/features/actions";
import { catalogQueryOptions } from "@/features/catalog";
import { apiProblems, type Problems } from "@/lib/api";
import { EntityPicker } from "./entity-picker";
import { InputField } from "./input-field";
import { type Draft, fromDraft, toDraft } from "./input-values";

export type RunTarget = { blueprint: string; identifier: string; title: string };

type RunActionDialogProps = {
  action: Available;
  // kind is workflow for a workflow, which runs on no entity, so the
  // form asks only for its inputs.
  kind?: RunKind;
  // entity is what it runs on. Without one, the form asks for an entity of
  // blueprint first, such as on a dashboard, which has no entity of its own.
  entity?: RunTarget;
  blueprint?: string;
  // inputs fill the form, such as a run's when running it again.
  inputs?: Record<string, unknown>;
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

// RunActionDialog asks for an action's inputs and runs it on an entity, or
// runs a workflow, then opens the run. A destructive action asks to be sure first.
export function RunActionDialog({ open, onOpenChange, ...props }: RunActionDialogProps) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        {/* Mounting only while open starts each run afresh. */}
        {open && <RunForm {...props} onDone={() => onOpenChange(false)} />}
      </DialogContent>
    </Dialog>
  );
}

function RunForm({
  action,
  kind = "action",
  entity,
  blueprint = entity?.blueprint ?? "",
  inputs = {},
  onDone,
}: Omit<RunActionDialogProps, "open" | "onOpenChange"> & { onDone: () => void }) {
  const [drafts, setDrafts] = useState<Record<string, Draft>>(() =>
    Object.fromEntries(
      action.inputs.map((input) => [input.identifier, toDraft(input, inputs[input.identifier])]),
    ),
  );
  const [problems, setProblems] = useState<Problems>({});
  const [picked, setPicked] = useState<{ identifier: string; title: string }>();
  const [unpicked, setUnpicked] = useState(false);
  const target = entity ?? picked;
  const needsEntity = kind === "action";
  const [confirming, setConfirming] = useState(false);
  // One key per form, so a request sent twice starts one run.
  const [key] = useState(() => crypto.randomUUID());
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const run = useMutation({
    mutationFn: (values: Record<string, unknown>) =>
      needsEntity
        ? startRun(action.identifier, {
            entity: target?.identifier ?? "",
            inputs: values,
            idempotencyKey: key,
          })
        : startWorkflow(action.identifier, { inputs: values, idempotencyKey: key }),
    onSuccess: async (started) => {
      await queryClient.invalidateQueries({ queryKey: runKeys.all });
      onDone();
      await navigate({ to: "/runs/$id", params: { id: started.id } });
    },
    onError: (error) => setProblems(apiProblems(error) ?? { "": [error.message] }),
  });

  function values() {
    const out: Record<string, unknown> = {};
    const wrong: Problems = {};
    for (const input of action.inputs) {
      const parsed = fromDraft(input, drafts[input.identifier]);
      if (parsed.problem) wrong[input.identifier] = [parsed.problem];
      else if (parsed.value !== undefined) out[input.identifier] = parsed.value;
    }
    setProblems(wrong);
    const missing = needsEntity && !target;
    setUnpicked(missing);
    return Object.keys(wrong).length > 0 || missing ? undefined : out;
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    const parsed = values();
    if (!parsed) return;
    if (action.destructive) setConfirming(true);
    else run.mutate(parsed);
  }

  function change(identifier: string, draft: Draft) {
    setDrafts((was) => ({ ...was, [identifier]: draft }));
    setProblems(({ [identifier]: _, ...rest }) => rest);
  }

  return (
    <form className="grid gap-6" onSubmit={submit} noValidate>
      <DialogHeader>
        <div className="flex items-center gap-3">
          <IconTile icon={action.icon} color={action.color} />
          <div className="grid gap-1">
            <DialogTitle className="flex items-center gap-2">
              {action.title}
              {action.destructive && <Badge variant="destructive">Destructive</Badge>}
            </DialogTitle>
            {target && <DialogDescription>{target.title}</DialogDescription>}
          </div>
        </div>
        {action.description && (
          <p className="text-sm text-muted-foreground">{action.description}</p>
        )}
      </DialogHeader>
      {((needsEntity && !entity) || action.inputs.length > 0) && (
        <FieldGroup>
          {needsEntity && !entity && (
            <PickEntity
              blueprint={blueprint}
              picked={picked}
              onPick={(next) => {
                setPicked(next);
                setUnpicked(false);
                // Whether it may run depends on the entity, so another one asks afresh.
                setProblems(({ "": _, ...rest }) => rest);
              }}
              invalid={unpicked}
            />
          )}
          {action.inputs.map((input) => (
            <InputField
              key={input.identifier}
              input={input}
              draft={drafts[input.identifier]}
              onChange={(draft) => change(input.identifier, draft)}
              problems={problems[input.identifier]}
            />
          ))}
        </FieldGroup>
      )}
      <FieldError errors={problems[""]?.map((message) => ({ message }))} />
      <DialogFooter>
        <DialogClose asChild>
          <Button type="button" variant="outline">
            Cancel
          </Button>
        </DialogClose>
        <Button
          type="submit"
          variant={action.destructive ? "destructive" : "default"}
          disabled={run.isPending}
        >
          {run.isPending ? <Loader2 className="animate-spin" /> : <Play />}
          Run
        </Button>
      </DialogFooter>
      <AlertDialog open={confirming} onOpenChange={setConfirming}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {target ? `Run ${action.title} on ${target.title}?` : `Run ${action.title}?`}
            </AlertDialogTitle>
            <AlertDialogDescription>
              It's marked destructive, so check the inputs before it runs.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                const parsed = values();
                if (parsed) run.mutate(parsed);
              }}
            >
              Run
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </form>
  );
}

// PickEntity is the form's first field when it has no entity to run on:
// one of the action's blueprint's, named by the blueprint.
function PickEntity({
  blueprint,
  picked,
  onPick,
  invalid,
}: Readonly<{
  blueprint: string;
  picked?: { identifier: string; title: string };
  onPick: (picked?: { identifier: string; title: string }) => void;
  invalid: boolean;
}>) {
  const { data: catalog } = useQuery(catalogQueryOptions);
  const title = catalog?.find((entry) => entry.identifier === blueprint)?.title ?? blueprint;
  return (
    <Field data-invalid={invalid || undefined}>
      <FieldLabel htmlFor="run-entity">
        {title}
        <span className="text-destructive" title="Required" aria-hidden>
          *
        </span>
      </FieldLabel>
      <EntityPicker
        id="run-entity"
        blueprint={blueprint}
        many={false}
        value={picked ? [picked.identifier] : []}
        onChange={([identifier], name) =>
          onPick(identifier ? { identifier, title: name || identifier } : undefined)
        }
        invalid={invalid}
        required
      />
      {invalid && <FieldError>Choose the {title.toLowerCase()} to run it on.</FieldError>}
    </Field>
  );
}
