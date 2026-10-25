// Colors are the names config documents color things with, such as tiers
// and property values. Class names are spelled out so Tailwind sees them.
export type Swatch = {
  // dot fills small marks and bars.
  dot: string;
  text: string;
  badge: string;
  // fill is the CSS color, for charts.
  fill: string;
};

const palette: Record<string, Swatch> = {
  gray: {
    dot: "bg-gray-400",
    text: "text-gray-600 dark:text-gray-300",
    badge: "border-transparent bg-gray-500/15 text-gray-700 dark:text-gray-300",
    fill: "var(--color-gray-400)",
  },
  red: {
    dot: "bg-red-500",
    text: "text-red-700 dark:text-red-400",
    badge: "border-transparent bg-red-500/15 text-red-700 dark:text-red-300",
    fill: "var(--color-red-500)",
  },
  orange: {
    dot: "bg-orange-500",
    text: "text-orange-700 dark:text-orange-400",
    badge: "border-transparent bg-orange-500/15 text-orange-700 dark:text-orange-300",
    fill: "var(--color-orange-500)",
  },
  amber: {
    dot: "bg-amber-400",
    text: "text-amber-700 dark:text-amber-300",
    badge: "border-transparent bg-amber-500/15 text-amber-700 dark:text-amber-300",
    fill: "var(--color-amber-400)",
  },
  yellow: {
    dot: "bg-yellow-400",
    text: "text-yellow-700 dark:text-yellow-300",
    badge: "border-transparent bg-yellow-500/15 text-yellow-700 dark:text-yellow-300",
    fill: "var(--color-yellow-400)",
  },
  lime: {
    dot: "bg-lime-500",
    text: "text-lime-700 dark:text-lime-400",
    badge: "border-transparent bg-lime-500/15 text-lime-700 dark:text-lime-300",
    fill: "var(--color-lime-500)",
  },
  green: {
    dot: "bg-green-500",
    text: "text-green-700 dark:text-green-400",
    badge: "border-transparent bg-green-500/15 text-green-700 dark:text-green-300",
    fill: "var(--color-green-500)",
  },
  emerald: {
    dot: "bg-emerald-500",
    text: "text-emerald-700 dark:text-emerald-400",
    badge: "border-transparent bg-emerald-500/15 text-emerald-700 dark:text-emerald-300",
    fill: "var(--color-emerald-500)",
  },
  teal: {
    dot: "bg-teal-500",
    text: "text-teal-700 dark:text-teal-400",
    badge: "border-transparent bg-teal-500/15 text-teal-700 dark:text-teal-300",
    fill: "var(--color-teal-500)",
  },
  cyan: {
    dot: "bg-cyan-500",
    text: "text-cyan-700 dark:text-cyan-400",
    badge: "border-transparent bg-cyan-500/15 text-cyan-700 dark:text-cyan-300",
    fill: "var(--color-cyan-500)",
  },
  sky: {
    dot: "bg-sky-400",
    text: "text-sky-700 dark:text-sky-300",
    badge: "border-transparent bg-sky-500/15 text-sky-700 dark:text-sky-300",
    fill: "var(--color-sky-400)",
  },
  blue: {
    dot: "bg-blue-500",
    text: "text-blue-700 dark:text-blue-400",
    badge: "border-transparent bg-blue-500/15 text-blue-700 dark:text-blue-300",
    fill: "var(--color-blue-500)",
  },
  indigo: {
    dot: "bg-indigo-500",
    text: "text-indigo-700 dark:text-indigo-400",
    badge: "border-transparent bg-indigo-500/15 text-indigo-700 dark:text-indigo-300",
    fill: "var(--color-indigo-500)",
  },
  violet: {
    dot: "bg-violet-500",
    text: "text-violet-700 dark:text-violet-400",
    badge: "border-transparent bg-violet-500/15 text-violet-700 dark:text-violet-300",
    fill: "var(--color-violet-500)",
  },
  purple: {
    dot: "bg-purple-500",
    text: "text-purple-700 dark:text-purple-400",
    badge: "border-transparent bg-purple-500/15 text-purple-700 dark:text-purple-300",
    fill: "var(--color-purple-500)",
  },
  fuchsia: {
    dot: "bg-fuchsia-500",
    text: "text-fuchsia-700 dark:text-fuchsia-400",
    badge: "border-transparent bg-fuchsia-500/15 text-fuchsia-700 dark:text-fuchsia-300",
    fill: "var(--color-fuchsia-500)",
  },
  pink: {
    dot: "bg-pink-500",
    text: "text-pink-700 dark:text-pink-400",
    badge: "border-transparent bg-pink-500/15 text-pink-700 dark:text-pink-300",
    fill: "var(--color-pink-500)",
  },
  rose: {
    dot: "bg-rose-500",
    text: "text-rose-700 dark:text-rose-400",
    badge: "border-transparent bg-rose-500/15 text-rose-700 dark:text-rose-300",
    fill: "var(--color-rose-500)",
  },
};

export function swatch(name: string | undefined): Swatch | undefined {
  return name ? palette[name] : undefined;
}

// categorical colors values that have none of their own, in turn.
export const categorical = ["blue", "emerald", "amber", "violet", "rose", "cyan", "lime", "orange"];
