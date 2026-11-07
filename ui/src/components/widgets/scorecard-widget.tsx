import { EmptyState } from "@/components/empty-state";
import { ChecksPassing } from "@/components/scorecards/checks-passing";
import { TierDistribution } from "@/components/scorecards/tier-distribution";
import type { TierData } from "@/features/widgets";
import { ViewAll, WidgetCard, WidgetError, WidgetLoading, type WidgetProps } from "./widget-grid";

// ScorecardWidget shows how many entities reached each tier of a scorecard,
// led by the share of their checks that pass, which moves with every check
// fixed, not only when an entity reaches a tier.
export function ScorecardWidget({ widget, context, data, error }: WidgetProps<TierData>) {
  if (error) return <WidgetError widget={widget} error={error} />;
  if (!data) return <WidgetLoading widget={widget} height="h-10" />;

  const total = data.tiers.reduce((sum, tier) => sum + tier.count, 0);
  const { passed, failed } = data.checks;
  return (
    <WidgetCard widget={widget} action={!context.draft && <ViewAll link={data.link} />}>
      {total === 0 ? (
        <EmptyState compact title="No scores yet" />
      ) : (
        <div className="grid gap-3">
          <ChecksPassing passed={passed} failed={failed} />
          <TierDistribution tiers={data.scorecard.tiers} counts={data.tiers} />
        </div>
      )}
    </WidgetCard>
  );
}
