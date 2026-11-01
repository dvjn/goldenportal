import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute, Link } from "@tanstack/react-router";
import { Plus, Trophy } from "lucide-react";
import { EmptyCard } from "@/components/empty-state";
import { LinkCard } from "@/components/link-card";
import { PageHeader } from "@/components/page-header";
import { ChecksPassing, checkTotals } from "@/components/scorecards/checks-passing";
import { TierDistribution } from "@/components/scorecards/tier-distribution";
import { Button } from "@/components/ui/button";
import { CardContent } from "@/components/ui/card";
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
      <PageHeader title="Scorecards" actions={newButton} />
      {shown.length === 0 ? (
        <EmptyCard
          icon={<Trophy />}
          title="No scorecards yet"
          description="A scorecard rates entities against tiers of checks, such as bronze, silver and gold."
        >
          {newButton}
        </EmptyCard>
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
    <LinkCard
      to="/scorecards/$identifier"
      params={{ identifier: scorecard.identifier }}
      icon={scorecard.icon}
      color={scorecard.color}
      title={scorecard.title}
      description={scorecard.description}
    >
      <CardContent className="grid gap-2">
        {!published ? (
          <p className="text-sm text-muted-foreground">Not published yet.</p>
        ) : summary.data ? (
          <>
            <div className="flex items-end justify-between gap-4">
              <ChecksPassing {...checkTotals(summary.data.criteria)} />
              <dl className="flex gap-4 text-xs text-muted-foreground">
                <div className="flex items-baseline gap-1">
                  <dd className="text-sm font-medium text-foreground tabular-nums">
                    {summary.data.entities}
                  </dd>
                  <dt>{summary.data.entities === 1 ? "entity" : "entities"}</dt>
                </div>
                <div className="flex items-baseline gap-1">
                  <dd className="text-sm font-medium text-foreground tabular-nums">
                    {summary.data.spec.tiers.flatMap((tier) => tier.criteria).length}
                  </dd>
                  <dt>checks</dt>
                </div>
              </dl>
            </div>
            <TierDistribution tiers={summary.data.spec.tiers} counts={summary.data.tiers} />
          </>
        ) : null}
      </CardContent>
    </LinkCard>
  );
}
