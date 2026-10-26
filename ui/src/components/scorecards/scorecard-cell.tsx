import type { Entity } from "@/features/catalog";
import type { ScorecardColumn } from "@/features/scorecards";
import { TierBadge } from "./tier-badge";

export function ScorecardCell({
  entity,
  column,
}: Readonly<{ entity: Entity; column?: ScorecardColumn }>) {
  const result = column && entity.scorecards?.[column.identifier];
  if (!column || !result)
    return <span className="text-sm text-muted-foreground">Not evaluated</span>;
  return <TierBadge tiers={column.tiers} name={result.tier} />;
}
