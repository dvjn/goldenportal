import { useQuery } from "@tanstack/react-query";
import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { RotateCcw } from "lucide-react";
import { useState } from "react";
import { ListSkeleton } from "@/components/common/list-skeleton";
import { PageHeader } from "@/components/common/page-header";
import { StateCard } from "@/components/common/state-card";
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
  type Revision,
  revisionQueryOptions,
  useDefinitions,
  usePlanRollback,
  useRevision,
} from "@/features/definitions";
import { formatDateTime, formatRelative } from "@/lib/time";
import { usePageTitle } from "@/lib/use-page-title";

type RevisionSearch = { compare?: number };

const noComparison = "none";

export const Route = createFileRoute("/_authenticated/settings/catalog-setup/history/$revision")({
  params: {
    parse: (params) => ({ revision: Number(params.revision) }),
    stringify: (params) => ({ revision: String(params.revision) }),
  },
  validateSearch: (search: Record<string, unknown>): RevisionSearch => {
    const compare = Math.floor(Number(search.compare));
    return { compare: search.compare !== undefined && compare >= 0 ? compare : undefined };
  },
  loaderDeps: ({ search }) => ({ compare: search.compare }),
  loader: ({ context, params, deps }) => {
    void context.queryClient.prefetchQuery(revisionQueryOptions(params.revision));
    void context.queryClient.prefetchQuery(definitionsQueryOptions);
    if (deps.compare !== undefined) {
      void context.queryClient.prefetchQuery(diffQueryOptions(deps.compare, params.revision));
    }
  },
  component: RevisionPage,
});

function RevisionPage() {
  const { revision: number } = Route.useParams();
  const revision = useRevision(number);
  const definitions = useDefinitions();
  usePageTitle(`Revision ${number}`);

  if (revision.isError || definitions.isError) {
    return (
      <>
        <PageHeader title={`Revision ${number}`} />
        <StateCard title="Revision unavailable" message="We could not load this revision." />
      </>
    );
  }
  if (!revision.data || !definitions.data) {
    return (
      <>
        <PageHeader title={`Revision ${number}`} />
        <ListSkeleton />
      </>
    );
  }

  return <RevisionContent revision={revision.data} current={definitions.data.revision} />;
}

function RevisionContent({ revision, current }: { revision: Revision; current: number }) {
  return (
    <>
      <PageHeader
        title={`Revision ${revision.number}`}
        description={
          <>
            {revision.number === current && (
              <Badge variant="secondary" className="mr-2">
                Current
              </Badge>
            )}
            {revision.rollback_of !== undefined && (
              <Badge variant="outline" className="mr-2">
                Rolled back to {revision.rollback_of}
              </Badge>
            )}
            {revision.message || "No message"}. Applied by {revision.author}{" "}
            <span title={formatDateTime(revision.created_at)}>
              {formatRelative(revision.created_at)}
            </span>
            , {appliedText(revision.summary)}.
          </>
        }
        actions={<RollBack number={revision.number} current={current} />}
      />
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
  const diff = useQuery(diffQueryOptions(from, to));
  if (diff.isError) {
    return <p className="text-destructive text-sm">We could not compare the revisions.</p>;
  }
  if (!diff.data) return <ListSkeleton />;
  return <ChangeList changes={diff.data.changes} />;
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
            to: "/settings/catalog-setup/history/$revision",
            params: { revision: applied.number },
            search: {},
          })
        }
        onReplan={planRollback}
      />
    </div>
  );
}
