import { cn } from "cn";
import type { Tier } from "@/features/scorecards";

// Class names are spelled out so Tailwind sees them; tiers name a color.
const colors: Record<string, { dot: string; bar: string; text: string }> = {
  gray: { dot: "bg-gray-400", bar: "bg-gray-400", text: "text-gray-600 dark:text-gray-300" },
  red: { dot: "bg-red-500", bar: "bg-red-500", text: "text-red-700 dark:text-red-400" },
  orange: {
    dot: "bg-orange-500",
    bar: "bg-orange-500",
    text: "text-orange-700 dark:text-orange-400",
  },
  amber: { dot: "bg-amber-400", bar: "bg-amber-400", text: "text-amber-700 dark:text-amber-300" },
  yellow: {
    dot: "bg-yellow-400",
    bar: "bg-yellow-400",
    text: "text-yellow-700 dark:text-yellow-300",
  },
  lime: { dot: "bg-lime-500", bar: "bg-lime-500", text: "text-lime-700 dark:text-lime-400" },
  green: { dot: "bg-green-500", bar: "bg-green-500", text: "text-green-700 dark:text-green-400" },
  emerald: {
    dot: "bg-emerald-500",
    bar: "bg-emerald-500",
    text: "text-emerald-700 dark:text-emerald-400",
  },
  teal: { dot: "bg-teal-500", bar: "bg-teal-500", text: "text-teal-700 dark:text-teal-400" },
  cyan: { dot: "bg-cyan-500", bar: "bg-cyan-500", text: "text-cyan-700 dark:text-cyan-400" },
  sky: { dot: "bg-sky-400", bar: "bg-sky-400", text: "text-sky-700 dark:text-sky-300" },
  blue: { dot: "bg-blue-500", bar: "bg-blue-500", text: "text-blue-700 dark:text-blue-400" },
  indigo: {
    dot: "bg-indigo-500",
    bar: "bg-indigo-500",
    text: "text-indigo-700 dark:text-indigo-400",
  },
  violet: {
    dot: "bg-violet-500",
    bar: "bg-violet-500",
    text: "text-violet-700 dark:text-violet-400",
  },
  purple: {
    dot: "bg-purple-500",
    bar: "bg-purple-500",
    text: "text-purple-700 dark:text-purple-400",
  },
  fuchsia: {
    dot: "bg-fuchsia-500",
    bar: "bg-fuchsia-500",
    text: "text-fuchsia-700 dark:text-fuchsia-400",
  },
  pink: { dot: "bg-pink-500", bar: "bg-pink-500", text: "text-pink-700 dark:text-pink-400" },
  rose: { dot: "bg-rose-500", bar: "bg-rose-500", text: "text-rose-700 dark:text-rose-400" },
};

// Tiers without a color get one by rank, so the ladder still reads bottom to top.
const ladder = ["orange", "sky", "amber", "emerald", "violet", "rose"];
const none = {
  dot: "bg-muted-foreground/30",
  bar: "bg-muted-foreground/25",
  text: "text-muted-foreground",
};

export function tierColors(tiers: Tier[], name: string) {
  const index = tiers.findIndex((tier) => tier.name === name);
  if (index < 0) return none;
  const color = tiers[index].color ?? ladder[index % ladder.length];
  return colors[color] ?? none;
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
