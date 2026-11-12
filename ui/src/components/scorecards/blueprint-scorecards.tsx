import { useSuspenseQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Plus, Trophy } from "lucide-react";
import { DocStatus } from "@/components/config-docs/doc-columns";
import { EmptyCard } from "@/components/empty-state";
import { LinkCard } from "@/components/link-card";
import { Button } from "@/components/ui/button";
import { CardContent } from "@/components/ui/card";
import { gradedBy, scorecards } from "@/features/scorecards";

const count = (n: number, noun: string) => `${n} ${n === 1 ? noun : `${noun}s`}`;

// BlueprintScorecards lists the scorecards that grade a blueprint, drafts
// too, for builders to open or start one.
export function BlueprintScorecards({ blueprint }: Readonly<{ blueprint: string }>) {
  const { data: list } = useSuspenseQuery(scorecards.queries.list);
  const graded = gradedBy(list, blueprint);
  const newButton = (
    <Button asChild>
      <Link to="/blueprints/$blueprint/scorecards/new" params={{ blueprint }}>
        <Plus />
        New Scorecard
      </Link>
    </Button>
  );

  if (graded.length === 0) {
    return (
      <EmptyCard
        icon={<Trophy />}
        title="No scorecards yet"
        description="A scorecard rates entities against tiers of checks, such as bronze, silver and gold."
      >
        {newButton}
      </EmptyCard>
    );
  }
  return (
    <div className="grid gap-4">
      <div className="flex justify-end">{newButton}</div>
      <ul className="grid gap-4 md:grid-cols-2">
        {graded.map((scorecard) => (
          <li key={scorecard.identifier}>
            <LinkCard
              to="/blueprints/$blueprint/scorecards/$identifier"
              params={{ blueprint, identifier: scorecard.identifier }}
              icon={scorecard.icon}
              color={scorecard.color}
              title={scorecard.title}
              description={scorecard.description}
            >
              <CardContent className="flex items-center justify-between gap-4">
                <DocStatus doc={scorecard} />
                <span className="text-xs text-muted-foreground tabular-nums">
                  {count(scorecard.stats.tiers, "tier")}, {count(scorecard.stats.criteria, "check")}
                </span>
              </CardContent>
            </LinkCard>
          </li>
        ))}
      </ul>
    </div>
  );
}
