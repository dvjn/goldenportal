import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { RotateCcw } from "lucide-react";
import { useState } from "react";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import {
  appliedText,
  ChangeList,
  definitionsQueryOptions,
  diffQueryOptions,
  ImpactList,
  type Plan,
  PlanDialog,
  revisionQueryOptions,
  useDefinitions,
  usePlanRollback,
  useRevision,
} from "@/features/definitions";
import { formatDateTime, formatRelative } from "@/lib/time";

type RevisionSearch = { compare?: number };

const noComparison = "none";

export const Route = createFileRoute("/_authenticated/definitions/history/$revision")({
  params: {
    parse: (params) => ({ revision: Number(params.revision) }),
    stringify: (params) => ({ revision: String(params.revision) }),
  },
  validateSearch: (search: Record<string, unknown>): RevisionSearch => {
    const compare = Math.floor(Number(search.compare));
    return { compare: search.compare !== undefined && compare >= 0 ? compare : undefined };
  },
  loaderDeps: ({ search }) => ({ compare: search.compare }),
  loader: async ({ context, params, deps }) => {
    const [revision] = await Promise.all([
      context.queryClient.ensureQueryData(revisionQueryOptions(params.revision)),
      context.queryClient.ensureQueryData(definitionsQueryOptions),
      deps.compare === undefined
        ? undefined
        : context.queryClient.ensureQueryData(diffQueryOptions(deps.compare, params.revision)),
    ]);
    return { breadcrumb: `Revision ${revision.number}` };
  },
  component: RevisionPage,
});

function RevisionPage() {
  const { revision: number } = Route.useParams();
  const revision = useRevision(number);
  const current = useDefinitions().revision;

  return (
    <>
      <header className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex flex-col gap-1">
          <h1 className="flex flex-wrap items-center gap-2 text-xl font-semibold">
            Revision {revision.number}
            {revision.number === current && <Badge variant="secondary">Current</Badge>}
            {revision.rollback_of !== undefined && (
              <Badge variant="outline">Rolled back to {revision.rollback_of}</Badge>
            )}
          </h1>
          <p className="text-muted-foreground text-sm">
            {revision.message || "No message"}. Applied by {revision.author}{" "}
            <span title={formatDateTime(revision.created_at)}>
              {formatRelative(revision.created_at)}
            </span>
            , {appliedText(revision.summary)}.
          </p>
        </div>
        <RollBack number={revision.number} current={current} />
      </header>
      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Changes</CardTitle>
            <CardDescription>What this revision changed in the definitions.</CardDescription>
          </CardHeader>
          <CardContent>
            <ChangeList changes={revision.changes} />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Entities</CardTitle>
            <CardDescription>What applying it did to the entities.</CardDescription>
          </CardHeader>
          <CardContent>
            <ImpactList impact={revision.impact} />
          </CardContent>
        </Card>
      </div>
      <Comparison number={revision.number} current={current} />
    </>
  );
}

function Comparison({ number, current }: { number: number; current: number }) {
  const { compare } = Route.useSearch();
  const navigate = useNavigate({ from: Route.fullPath });
  const others = Array.from({ length: current + 1 }, (_, index) => current - index).filter(
    (other) => other !== number,
  );

  return (
    <Card>
      <CardHeader>
        <CardTitle>Compare</CardTitle>
        <CardDescription>The changes from another revision to revision {number}.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <Select
          value={compare === undefined ? noComparison : String(compare)}
          onValueChange={(value) =>
            navigate({ search: { compare: value === noComparison ? undefined : Number(value) } })
          }
        >
          <SelectTrigger aria-label="Compare with" className="sm:w-72">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={noComparison}>Choose a revision to compare with</SelectItem>
            {others.map((other) => (
              <SelectItem key={other} value={String(other)}>
                {other === 0 ? "No definitions, before revision 1" : `Revision ${other}`}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {compare !== undefined && <Diff from={compare} to={number} />}
      </CardContent>
    </Card>
  );
}

function Diff({ from, to }: { from: number; to: number }) {
  const diff = useSuspenseQuery(diffQueryOptions(from, to)).data;
  return <ChangeList changes={diff.changes} />;
}

function RollBack({ number, current }: { number: number; current: number }) {
  const rollback = usePlanRollback();
  const [plan, setPlan] = useState<Plan>();
  const navigate = useNavigate();

  if (number === current) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <span>
            <Button variant="outline" disabled>
              <RotateCcw />
              Roll back to this revision
            </Button>
          </span>
        </TooltipTrigger>
        <TooltipContent>This is the current revision</TooltipContent>
      </Tooltip>
    );
  }

  function planRollback() {
    rollback.mutate(number, { onSuccess: setPlan });
  }

  return (
    <div className="flex flex-col items-end gap-2">
      <Button variant="outline" disabled={rollback.isPending} onClick={planRollback}>
        <RotateCcw />
        Roll back to this revision
      </Button>
      {rollback.isError && (
        <Alert variant="destructive">
          <AlertDescription>{rollback.error.message}</AlertDescription>
        </Alert>
      )}
      <PlanDialog
        plan={plan}
        onOpenChange={(open) => !open && setPlan(undefined)}
        onApplied={(applied) =>
          navigate({
            to: "/definitions/history/$revision",
            params: { revision: applied.number },
            search: {},
          })
        }
        onReplan={planRollback}
      />
    </div>
  );
}
