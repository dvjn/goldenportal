import { cn } from "cn";
import type { Tier } from "@/features/scorecards";
import { swatch } from "@/lib/colors";

// Tiers without a color get one by rank, so the ladder still reads bottom to top.
const ladder = ["orange", "sky", "amber", "emerald", "violet", "rose"];
const none = { dot: "bg-muted-foreground/30", text: "text-muted-foreground" };

export function tierColors(tiers: Tier[], name: string) {
  const index = tiers.findIndex((tier) => tier.name === name);
  if (index < 0) return none;
  return swatch(tiers[index].color ?? ladder[index % ladder.length]) ?? none;
}

export function tierTitle(tiers: Tier[], name: string) {
  return tiers.find((tier) => tier.name === name)?.title ?? "No tier";
}

// TierBadge shows a tier with its color, or "No tier" when none was reached.
export function TierBadge({
  tiers,
  name,
  className,
}: Readonly<{ tiers: Tier[]; name: string; className?: string }>) {
  const color = tierColors(tiers, name);
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 text-sm font-medium whitespace-nowrap",
        color.text,
        className,
      )}
    >
      <span className={cn("size-2 shrink-0 rounded-full", color.dot)} aria-hidden />
      {tierTitle(tiers, name)}
    </span>
  );
}
