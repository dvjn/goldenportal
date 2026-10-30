const units: [Intl.RelativeTimeFormatUnit, number][] = [
  ["year", 365 * 24 * 60 * 60],
  ["month", 30 * 24 * 60 * 60],
  ["day", 24 * 60 * 60],
  ["hour", 60 * 60],
  ["minute", 60],
];

const relative = new Intl.RelativeTimeFormat("en", { numeric: "auto" });
const absolute = new Intl.DateTimeFormat("en", { dateStyle: "medium", timeStyle: "short" });

// formatRelative turns a timestamp into "in 3 days" or "5 minutes ago".
export function formatRelative(value: string | Date, now = new Date()) {
  const seconds = Math.round((new Date(value).getTime() - now.getTime()) / 1000);
  for (const [unit, size] of units) {
    if (Math.abs(seconds) >= size) {
      return relative.format(Math.round(seconds / size), unit);
    }
  }
  return "just now";
}

export function formatAbsolute(value: string | Date) {
  return absolute.format(new Date(value));
}

// Dates have no time, so they're read and written in UTC to stay on their day.
const dates = new Intl.DateTimeFormat("en", { dateStyle: "medium", timeZone: "UTC" });

export function formatDate(value: string | Date) {
  return dates.format(new Date(value));
}

// formatDuration says how long something took between two timestamps, such as "850ms" or "2m 5s".
export function formatDuration(start: string | Date, end: string | Date) {
  const ms = Math.max(new Date(end).getTime() - new Date(start).getTime(), 0);
  if (ms < 1000) return `${ms}ms`;
  const seconds = Math.round(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  return seconds % 60 === 0 ? `${minutes}m` : `${minutes}m ${seconds % 60}s`;
}
