import { useMutation, useQueryClient } from "@tanstack/react-query";
import { cn } from "cn";
import { Clock, Loader2, ThumbsDown, ThumbsUp } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Markdown } from "@/components/markdown";
import { Button } from "@/components/ui/button";
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field";
import { Textarea } from "@/components/ui/textarea";
import {
  approvalDetail,
  type Decision,
  decideRun,
  type Review,
  type Run,
  type RunStep,
  runKeys,
} from "@/features/actions";
import { formatAbsolute, formatRelative } from "@/lib/format";
import { blockHeading, JsonBlock } from "./json-block";

// Thumbs up and down are approvers' decisions everywhere, as a rejected
// run's status is a thumbs down.
const decisionLooks = {
  approve: { label: "Approved", icon: ThumbsUp, className: "text-success" },
  reject: { label: "Rejected", icon: ThumbsDown, className: "text-destructive" },
};

// ApprovalView is an approval step as approvers see it: what it asks, what
// to review, how many approved out of how many it needs, and who decided
// what. It stays the same once the step is over, so the record reads as
// it was decided. Those who can decide now do so at the end.
export function ApprovalView({ run, step }: Readonly<{ run: Run; step: RunStep }>) {
  const detail = approvalDetail(step);
  const decisions = step.decisions ?? [];
  const approvals = decisions.filter((d) => d.decision === "approve").length;
  const waiting = step.status === "waiting";

  return (
    <div className="grid gap-3">
      {detail?.message && <p className="text-sm">{detail.message}</p>}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
        {detail && (
          <span
            className="inline-flex items-center gap-1.5 tabular-nums"
            title={`${approvals} of ${detail.require} approvals needed`}
          >
            <ThumbsUp className="size-4 text-muted-foreground" aria-label="Approvals" />
            <span className="font-medium">{approvals}</span>
            <span className="text-muted-foreground">/ {detail.require}</span>
          </span>
        )}
        {waiting && step.dueAt && (
          <span
            className="inline-flex items-center gap-1.5 text-muted-foreground"
            title={formatAbsolute(step.dueAt)}
          >
            <Clock className="size-4" aria-label="Due" />
            {formatRelative(step.dueAt)}
          </span>
        )}
      </div>
      {detail?.review?.map((review, index) => (
        // Review blocks needn't have titles, and their order is what they are.
        // biome-ignore lint/suspicious/noArrayIndexKey: review blocks have no identity of their own.
        <ReviewBlock key={index} review={review} />
      ))}
      {decisions.length > 0 && (
        <ul className="grid gap-2" aria-label="Decisions">
          {decisions.map((decision) => (
            <DecisionItem key={decision.email} decision={decision} />
          ))}
        </ul>
      )}
      {waiting && run.canDecide && <DecideForm run={run} />}
    </div>
  );
}

function DecisionItem({ decision }: Readonly<{ decision: Decision }>) {
  const look = decisionLooks[decision.decision];
  return (
    <li className="grid grid-cols-[1rem_minmax(0,1fr)] gap-x-2 text-sm">
      <look.icon className={cn("mt-0.5 size-4", look.className)} aria-label={look.label} />
      <div className="grid gap-0.5">
        <span className="flex flex-wrap items-baseline gap-x-2">
          <span className="font-medium" title={decision.email}>
            {decision.name || decision.email}
          </span>
          <span
            className="text-xs text-muted-foreground"
            title={formatAbsolute(decision.decidedAt)}
          >
            {formatRelative(decision.decidedAt)}
          </span>
        </span>
        {decision.comment && <p className="text-muted-foreground">{decision.comment}</p>}
      </div>
    </li>
  );
}

// ReviewBlock is one thing to review: markdown as written, or JSON.
function ReviewBlock({ review }: Readonly<{ review: Review }>) {
  if (review.json !== undefined) return <JsonBlock title={review.title} value={review.json} />;
  return (
    <section className="grid gap-1.5">
      {review.title && <h4 className={blockHeading}>{review.title}</h4>}
      <div className="rounded-md border px-3 py-2">
        <Markdown>{review.markdown ?? ""}</Markdown>
      </div>
    </section>
  );
}

// DecideForm approves or rejects the step. Rejecting says why, for whoever
// asked for the run.
function DecideForm({ run }: Readonly<{ run: Run }>) {
  const [comment, setComment] = useState("");
  const queryClient = useQueryClient();
  const decide = useMutation({
    mutationFn: (decision: Decision["decision"]) => decideRun(run.id, { decision, comment }),
    onSuccess: async (changed, decision) => {
      queryClient.setQueryData(runKeys.detail(run.id), changed);
      await queryClient.invalidateQueries({ queryKey: runKeys.all });
      toast.success(decision === "approve" ? "Approved the run" : "Rejected the run");
    },
  });
  const pending = decide.isPending;

  return (
    <div className="grid gap-3 border-t pt-3">
      <Field>
        <FieldLabel htmlFor="decide-comment">Comment</FieldLabel>
        <Textarea
          id="decide-comment"
          value={comment}
          onChange={(event) => setComment(event.target.value)}
          rows={2}
        />
        <FieldDescription>Needed to reject.</FieldDescription>
        <FieldError>{decide.error?.message}</FieldError>
      </Field>
      <div className="flex justify-end gap-2">
        <Button
          variant="destructive"
          disabled={pending || comment.trim() === ""}
          onClick={() => decide.mutate("reject")}
        >
          {pending && decide.variables === "reject" ? (
            <Loader2 className="animate-spin" />
          ) : (
            <ThumbsDown />
          )}
          Reject
        </Button>
        <Button disabled={pending} onClick={() => decide.mutate("approve")}>
          {pending && decide.variables === "approve" ? (
            <Loader2 className="animate-spin" />
          ) : (
            <ThumbsUp />
          )}
          Approve
        </Button>
      </div>
    </div>
  );
}
