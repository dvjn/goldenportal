import { HTTPError } from "ky";
import { type FormEvent, useState } from "react";
import { Alert, AlertAction, AlertDescription } from "@/components/ui/alert";
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
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import type { Plan, Revision } from "./api";
import { ChangeList, ImpactList, summaryText } from "./plan-view";
import { useApplyPlan } from "./queries";

type PlanDialogProps = {
  plan: Plan | undefined;
  onOpenChange: (open: boolean) => void;
  onApplied: (revision: Revision) => void;
  onReplan: () => void;
};

// A plan that expired or that another change overtook cannot be applied;
// the server answers 404 or 409 and the person plans again.
function needsNewPlan(error: Error | null) {
  return error instanceof HTTPError && [404, 409].includes(error.response.status);
}

export function PlanDialog({ plan, onOpenChange, onApplied, onReplan }: PlanDialogProps) {
  const [message, setMessage] = useState("");
  const apply = useApplyPlan();

  function close(open: boolean) {
    if (!open) {
      apply.reset();
      setMessage("");
    }
    onOpenChange(open);
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    if (!plan?.id) return;
    apply.mutate(
      { planId: plan.id, message },
      {
        onSuccess: (revision) => {
          close(false);
          onApplied(revision);
        },
      },
    );
  }

  return (
    <Dialog open={plan !== undefined} onOpenChange={close}>
      <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-3xl">
        {plan && (
          <form onSubmit={submit} className="flex flex-col gap-6">
            <DialogHeader>
              <DialogTitle>
                {plan.rollback_of ? `Roll back to revision ${plan.rollback_of}` : "Plan"}
              </DialogTitle>
              <DialogDescription>
                {plan.id
                  ? `${summaryText(plan.summary)}. Nothing changes until you apply.`
                  : "Nothing to apply. The definitions already match."}
              </DialogDescription>
            </DialogHeader>
            {plan.id && (
              <>
                <section className="flex flex-col gap-2">
                  <h3 className="text-sm font-semibold">Documents</h3>
                  <ChangeList changes={plan.changes} />
                </section>
                <section className="flex flex-col gap-2">
                  <h3 className="text-sm font-semibold">Entities</h3>
                  <ImpactList impact={plan.impact} />
                </section>
                <Field>
                  <FieldLabel htmlFor="plan-message">Message (optional)</FieldLabel>
                  <Input
                    id="plan-message"
                    maxLength={500}
                    placeholder="Why the definitions change"
                    value={message}
                    onChange={(event) => setMessage(event.target.value)}
                  />
                </Field>
              </>
            )}
            {apply.isError && (
              <Alert variant="destructive">
                <AlertDescription>{apply.error.message}</AlertDescription>
                {needsNewPlan(apply.error) && (
                  <AlertAction>
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      onClick={() => {
                        close(false);
                        onReplan();
                      }}
                    >
                      Plan again
                    </Button>
                  </AlertAction>
                )}
              </Alert>
            )}
            <DialogFooter>
              <DialogClose asChild>
                <Button type="button" variant="outline">
                  {plan.id ? "Cancel" : "Close"}
                </Button>
              </DialogClose>
              {plan.id && (
                <Button type="submit" disabled={apply.isPending}>
                  Apply
                </Button>
              )}
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
