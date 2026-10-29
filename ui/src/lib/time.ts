const relative = new Intl.RelativeTimeFormat("en", { numeric: "auto" });

const units: [Intl.RelativeTimeFormatUnit, number][] = [
  ["year", 365 * 24 * 3600],
  ["month", 30 * 24 * 3600],
  ["day", 24 * 3600],
  ["hour", 3600],
  ["minute", 60],
];

export const notSet = "Not set";

export function formatRelative(iso: string | null | undefined, now = Date.now()) {
  if (!iso) return notSet;
  const timestamp = new Date(iso).getTime();
  if (Number.isNaN(timestamp)) return iso;

  const seconds = Math.round((timestamp - now) / 1000);
  const match = units.find(([, size]) => Math.abs(seconds) >= size);
  if (!match) return "just now";
  const [unit, size] = match;
  return relative.format(Math.round(seconds / size), unit);
}

export function formatDateTime(iso: string | null | undefined) {
  if (!iso) return notSet;
  return new Date(iso).toLocaleString("en", { dateStyle: "medium", timeStyle: "short" });
}

export function formatDuration(seconds: number) {
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  return [hours && `${hours} h`, minutes && `${minutes} min`].filter(Boolean).join(" ") || "0 min";
}

export function formatElapsed(startedAt: string, finishedAt: string | null | undefined) {
  if (!finishedAt) return notSet;
  const seconds = Math.round(
    (new Date(finishedAt).getTime() - new Date(startedAt).getTime()) / 1000,
  );
  if (seconds < 60) return `${seconds}s`;
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
}

export function isOlderThan(iso: string, seconds: number, now = Date.now()) {
  return now - new Date(iso).getTime() > seconds * 1000;
}
