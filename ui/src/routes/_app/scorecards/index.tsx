import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { Plus, Trophy } from "lucide-react";
import { BlueprintIcon } from "@/components/catalog/blueprint-icon";
import { PageHeader } from "@/components/page-header";
import { TierDistribution } from "@/components/scorecards/tier-distribution";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { hasRole, useMe } from "@/features/auth";
import { type ScorecardSummary, scorecards, summaryQueryOptions } from "@/features/scorecards";

export const Route = createFileRoute("/_app/scorecards/")({
  loader: ({ context }) => context.queryClient.ensureQueryData(scorecards.queries.list),
  component: ScorecardsPage,
});

function ScorecardsPage() {
  const { data: list } = useSuspenseQuery(scorecards.queries.list);
  const me = useMe();
  const builder = hasRole(me.role, "builder");
  // Members only see what's in effect; builders also see drafts they're writing.
  const shown = builder ? list : list.filter((s) => s.publishedVersion > 0);
  const newButton = builder && (
    <Button asChild>
      <Link to="/scorecards/new">
        <Plus />
        New Scorecard
      </Link>
    </Button>
  );

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Scorecards"
        description="How entities measure up, tier by tier."
        actions={newButton}
      />
      {shown.length === 0 ? (
        <Card className="py-0">
          <Empty>
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <Trophy />
              </EmptyMedia>
              <EmptyTitle>No scorecards yet</EmptyTitle>
              <EmptyDescription>
                A scorecard rates entities against tiers of checks, such as bronze, silver and gold.
              </EmptyDescription>
            </EmptyHeader>
            {newButton && <EmptyContent>{newButton}</EmptyContent>}
          </Empty>
        </Card>
      ) : (
        <ul className="grid gap-4 md:grid-cols-2">
          {shown.map((scorecard) => (
            <li key={scorecard.identifier}>
              <ScorecardCard scorecard={scorecard} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function ScorecardCard({ scorecard }: Readonly<{ scorecard: ScorecardSummary }>) {
  const published = scorecard.publishedVersion > 0;
  const summary = useQuery({ ...summaryQueryOptions(scorecard.identifier), enabled: published });

  return (
    <Link
      to="/scorecards/$identifier"
      params={{ identifier: scorecard.identifier }}
      className="block h-full rounded-xl focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
    >
      <Card className="h-full transition-colors hover:bg-accent/50">
        <CardHeader>
          <div className="flex items-start gap-3">
            <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted">
              <BlueprintIcon icon={scorecard.icon} className="size-4.5" />
            </span>
            <div className="grid min-w-0 gap-1">
              <CardTitle className="truncate">{scorecard.title}</CardTitle>
              {scorecard.description && (
                <CardDescription className="line-clamp-2">{scorecard.description}</CardDescription>
              )}
            </div>
          </div>
        </CardHeader>
        <CardContent className="grid gap-2">
          {!published ? (
            <p className="text-sm text-muted-foreground">Not published yet.</p>
          ) : summary.data ? (
            <>
              <p className="text-xs text-muted-foreground">
                {summary.data.entities} {summary.data.spec.blueprint} entities ·{" "}
                {summary.data.criteria.length} criteria
              </p>
              <TierDistribution tiers={summary.data.spec.tiers} counts={summary.data.tiers} />
            </>
          ) : null}
        </CardContent>
      </Card>
    </Link>
  );
}
