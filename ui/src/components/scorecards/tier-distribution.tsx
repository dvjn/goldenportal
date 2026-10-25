import { cn } from "cn";
import { TierBadge, tierColors } from "@/components/scorecards/tier-badge";
import type { Summary, Tier } from "@/features/scorecards";

// TierDistribution is one bar split by how many entities reached each tier,
// best tier first, with a legend of counts.
export function TierDistribution({
  tiers,
  counts,
  legend = true,
}: Readonly<{ tiers: Tier[]; counts: Summary["tiers"]; legend?: boolean }>) {
  const ordered = [...counts].reverse();
  const total = counts.reduce((sum, tier) => sum + tier.count, 0);

  return (
    <div className="grid gap-2">
      <div
        className="flex h-2.5 overflow-hidden rounded-full bg-muted"
        role="img"
        aria-label="Tier distribution"
      >
        {total > 0 &&
          ordered
            .filter((tier) => tier.count > 0)
            .map((tier) => (
              <span
                key={tier.name || "none"}
                className={cn("h-full", tierColors(tiers, tier.name).bar)}
                style={{ width: `${(tier.count / total) * 100}%` }}
              />
            ))}
      </div>
      {legend && (
        <div className="flex flex-wrap gap-x-4 gap-y-1">
          {ordered.map((tier) => (
            <span key={tier.name || "none"} className="flex items-center gap-1.5 text-sm">
              <TierBadge tiers={tiers} name={tier.name} />
              <span className="text-muted-foreground tabular-nums">{tier.count}</span>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
