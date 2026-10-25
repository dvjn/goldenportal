import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { ArrowRight } from "lucide-react";
import { TierDistribution } from "@/components/scorecards/tier-distribution";
import { scorecards } from "@/features/scorecards";
import { type TierData, widgetQueryOptions } from "@/features/widgets";
import { WidgetCard, WidgetError, WidgetLoading, type WidgetProps } from "./widget-grid";

// ScorecardWidget shows how many entities reached each tier of a scorecard.
export function ScorecardWidget({ source, widget, context }: WidgetProps) {
  const data = useQuery(widgetQueryOptions<TierData>(source, widget.identifier, context));
  const { data: doc } = useQuery(scorecards.queries.detail(widget.scorecard ?? ""));
  if (data.error) return <WidgetError widget={widget} error={data.error} />;
  const spec = doc?.published?.spec;
  if (!data.data || !spec) return <WidgetLoading widget={widget} height="h-10" />;

  const total = data.data.tiers.reduce((sum, tier) => sum + tier.count, 0);
  const top = data.data.tiers.at(-1)?.count ?? 0;
  const link = (
    <Link
      to="/scorecards/$identifier"
      params={{ identifier: spec.identifier }}
      className="flex items-center gap-1 text-muted-foreground hover:text-foreground"
    >
      Scorecard
      <ArrowRight className="size-3.5" aria-hidden />
    </Link>
  );

  return (
    <WidgetCard widget={widget} action={link}>
      {total === 0 ? (
        <p className="text-sm text-muted-foreground">Nothing is scored yet.</p>
      ) : (
        <div className="grid gap-3">
          <p className="text-sm text-muted-foreground">
            <span className="text-2xl font-semibold text-foreground tabular-nums">
              {Math.round((top / total) * 100)}%
            </span>{" "}
            reached {spec.tiers.at(-1)?.title}, of {total} scored
          </p>
          <TierDistribution tiers={spec.tiers} counts={data.data.tiers} />
        </div>
      )}
    </WidgetCard>
  );
}
