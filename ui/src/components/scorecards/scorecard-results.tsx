import { useQuery } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { CardTitleBar } from "@/components/card-sections";
import type { TableSearch } from "@/components/data-table";
import { ChecksPassing, checkTotals } from "@/components/scorecards/checks-passing";
import { AboutCard, CriteriaCard } from "@/components/scorecards/scorecard-cards";
import { ScorecardEntities } from "@/components/scorecards/scorecard-entities";
import { TierDistribution } from "@/components/scorecards/tier-distribution";
import { Card, CardContent } from "@/components/ui/card";
import { type ScorecardSpec, scorecards, useSummary } from "@/features/scorecards";

type ResultsProps = {
  search: TableSearch;
  // path reaches the scorecard's result on the blueprint's entities.
  path: string;
};

// ScorecardResults is unfiltered; its counts link to the entities tab, which filters.
export function ScorecardResults({
  identifier,
  ...props
}: Readonly<ResultsProps & { identifier: string }>) {
  const { data: doc } = useQuery(scorecards.queries.detail(identifier));
  const spec = doc?.published?.spec;
  if (!spec) {
    return <Loader2 className="size-4 animate-spin text-muted-foreground" aria-label="Loading" />;
  }
  return <Results spec={spec} {...props} />;
}

const uncounted = "Can't count these entities.";

function Results({ spec, search, path }: Readonly<ResultsProps & { spec: ScorecardSpec }>) {
  const summary = useSummary(spec.identifier);
  const link = { blueprint: spec.blueprint, path };

  return (
    <div className="grid grid-cols-1 gap-4">
      <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
        <Card>
          <CardTitleBar title="Tiers" />
          <CardContent className="grid gap-3">
            {summary.data ? (
              <>
                <ChecksPassing {...checkTotals(summary.data.criteria)} />
                <TierDistribution tiers={spec.tiers} counts={summary.data.tiers} link={link} />
              </>
            ) : summary.isError ? (
              <p className="text-sm text-muted-foreground">{uncounted}</p>
            ) : (
              <Loader2 className="size-4 animate-spin text-muted-foreground" aria-label="Loading" />
            )}
          </CardContent>
        </Card>
        <AboutCard spec={spec} summary={summary.data} />
      </div>
      {summary.isError ? (
        <CriteriaCard spec={spec} notice={uncounted} />
      ) : (
        <CriteriaCard spec={spec} results={{ summary: summary.data, link }} />
      )}
      <ScorecardEntities spec={spec} search={search} />
    </div>
  );
}
