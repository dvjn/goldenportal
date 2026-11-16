import { cn } from "cn";
import { TierBadge, tierColors } from "@/components/scorecards/tier-badge";
import { Toggle } from "@/components/ui/toggle";
import type { Summary, Tier } from "@/features/scorecards";

// TierDistribution is one bar split by how many entities reached each tier,
// best tier first, with a legend of counts. With onPick, each legend entry
// toggles its tier, "" for none, in or out of those picked, pressed while
// picked lists it.
export function TierDistribution({
  tiers,
  counts,
  legend = true,
  picked = [],
  onPick,
}: Readonly<{
  tiers: Tier[];
  counts: Summary["tiers"];
  legend?: boolean;
  picked?: string[];
  onPick?: (tier: string) => void;
}>) {
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
                className={cn("h-full", tierColors(tiers, tier.name).dot)}
                style={{ width: `${(tier.count / total) * 100}%` }}
              />
            ))}
      </div>
      {legend && (
        <div className={cn("flex flex-wrap gap-y-1", onPick ? "-ml-2.5 gap-x-3" : "gap-x-4")}>
          {ordered.map((tier) => (
            // The count stands beside the toggle, so the toggle reads as its tier.
            <span key={tier.name || "none"} className="flex items-center gap-1.5 text-sm">
              {onPick ? (
                <Toggle
                  size="sm"
                  className="text-sm"
                  pressed={picked.includes(tier.name)}
                  onPressedChange={() => onPick(tier.name)}
                >
                  <TierBadge tiers={tiers} name={tier.name} />
                </Toggle>
              ) : (
                <TierBadge tiers={tiers} name={tier.name} />
              )}
              <span className="text-muted-foreground tabular-nums">{tier.count}</span>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
