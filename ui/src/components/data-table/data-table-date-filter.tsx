// A range is days, YYYY-MM-DD in the viewer's time zone with to including
// that day, or within, a duration such as 7d reaching back from now, which
// is how a filter says it too.
export type DateRange = { from?: string; to?: string; within?: string };

const durationPattern = /^([1-9]\d{0,5})([mhdw])$/;
const durationUnits: Record<string, [ms: number, unit: string]> = {
  m: [60_000, "minute"],
  h: [3_600_000, "hour"],
  d: [86_400_000, "day"],
  w: [604_800_000, "week"],
};

// isDuration reports a within duration, such as 30m, 12h, 7d or 2w.
export function isDuration(text: unknown): text is string {
  return typeof text === "string" && durationPattern.test(text);
}

export function isoDate(date: Date) {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

function daysAgo(days: number) {
  const date = new Date();
  date.setDate(date.getDate() - days);
  return isoDate(date);
}

export const datePresets: { label: string; range: () => DateRange }[] = [
  { label: "Today", range: () => ({ from: isoDate(new Date()), to: isoDate(new Date()) }) },
  { label: "Last 24 hours", range: () => ({ within: "24h" }) },
  { label: "Last 7 days", range: () => ({ within: "7d" }) },
  { label: "Last 30 days", range: () => ({ within: "30d" }) },
  { label: "Last 90 days", range: () => ({ within: "90d" }) },
  { label: "Older than 90 days", range: () => ({ to: daysAgo(90) }) },
];

// withinStart is the instant a within duration reaches back to.
export function withinStart(within: string, now = new Date()) {
  const [, count, unit] = durationPattern.exec(within) ?? [];
  return new Date(now.getTime() - Number(count) * (durationUnits[unit]?.[0] ?? 0));
}

// dateRangeBounds turns a range into the instants it starts and ends: from
// is the start of its day, or as far back as within reaches, and before is
// the start of the day after to.
export function dateRangeBounds({ from, to, within }: DateRange): {
  from?: string;
  before?: string;
} {
  if (within) return { from: withinStart(within).toISOString() };
  const start = (date: string) => new Date(`${date}T00:00:00`);
  const dayAfter = (date: string) => {
    const next = start(date);
    next.setDate(next.getDate() + 1);
    return next;
  };
  return {
    from: from ? start(from).toISOString() : undefined,
    before: to ? dayAfter(to).toISOString() : undefined,
  };
}

const shortDate = new Intl.DateTimeFormat("en", { month: "short", day: "numeric" });

export function describeDateRange({ from, to, within }: DateRange) {
  if (within) {
    const [, count, unit] = durationPattern.exec(within) ?? [];
    const name = durationUnits[unit]?.[1] ?? unit;
    return count === "1" ? `Last ${name}` : `Last ${count} ${name}s`;
  }
  const show = (date: string) => shortDate.format(new Date(`${date}T00:00:00`));
  if (from && to) return from === to ? show(from) : `${show(from)} - ${show(to)}`;
  if (from) return `From ${show(from)}`;
  if (to) return `Until ${show(to)}`;
  return "";
}
