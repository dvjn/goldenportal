import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useRef } from "react";
import { CardTitleBar } from "@/components/card-sections";
import { type CatalogFilters, checkPath } from "@/components/catalog/catalog-filters";
import type { TableSearch } from "@/components/data-table";
import { ChecksPassing, checkTotals } from "@/components/scorecards/checks-passing";
import { AboutCard, CriteriaCard } from "@/components/scorecards/scorecard-cards";
import { ScorecardEntities } from "@/components/scorecards/scorecard-entities";
import { TierDistribution } from "@/components/scorecards/tier-distribution";
import { Card, CardContent } from "@/components/ui/card";
import type { Filter } from "@/features/catalog";
import { type ScorecardSpec, scorecards, useSummary } from "@/features/scorecards";

type ResultsProps = {
  search: TableSearch;
  // where is the catalog page's filter, which the tiers, counts and entities follow.
  where?: Filter;
  // path reaches the scorecard's result on the blueprint's entities, for
  // the filter to pick a tier or a check's status by.
  path: string;
  catalogFilters: CatalogFilters;
};

// ScorecardResults shows how a blueprint's entities fare on one of its
// scorecards: how many reached each tier, how each criterion fares, and
// every entity's result. A tier or a criterion's count filters to them.
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

function Results({
  spec,
  search,
  where,
  path,
  catalogFilters,
}: Readonly<ResultsProps & { spec: ScorecardSpec }>) {
  const summary = useSummary(spec.identifier, where, { placeholderData: keepPreviousData });
  const entities = useRef<HTMLDivElement>(null);
  const show = (path: string, value: string) => {
    catalogFilters.set(path, [value]);
    entities.current?.scrollIntoView({ behavior: "smooth", block: "start" });
  };

  return (
    <div className="grid gap-4">
      <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
        <Card>
          <CardTitleBar title="Tiers" />
          <CardContent className="grid gap-3">
            {summary.data ? (
              <>
                <ChecksPassing {...checkTotals(summary.data.criteria)} />
                <TierDistribution
                  tiers={spec.tiers}
                  counts={summary.data.tiers}
                  onPick={(tier) => show(path, tier)}
                />
              </>
            ) : (
              <Loader2 className="size-4 animate-spin text-muted-foreground" aria-label="Loading" />
            )}
          </CardContent>
        </Card>
        <AboutCard spec={spec} summary={summary.data} />
      </div>
      <CriteriaCard
        spec={spec}
        results={{
          summary: summary.data,
          onPick: (criterion, status) => show(checkPath(path, criterion), status),
        }}
      />
      <div ref={entities} className="scroll-mt-16">
        <ScorecardEntities
          spec={spec}
          search={search}
          where={where}
          catalogFilters={catalogFilters}
        />
      </div>
    </div>
  );
}
