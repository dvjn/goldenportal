import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { EntityFilter } from "../api";

const dayMs = 86_400_000;

type DatePreset = { value: string; label: string; bound: "gte" | "lte"; days: number };

export const datePresets: DatePreset[] = [
  { value: "last-7", label: "Last 7 days", bound: "gte", days: 7 },
  { value: "last-30", label: "Last 30 days", bound: "gte", days: 30 },
  { value: "last-90", label: "Last 90 days", bound: "gte", days: 90 },
  { value: "older-90", label: "Older than 90 days", bound: "lte", days: 90 },
];

const anyValue = "any";
const customValue = "custom";

function daysAgo(days: number, now: number): string {
  return new Date(now - days * dayMs).toISOString().slice(0, 10);
}

export function datePresetFilter(presetValue: string, now = Date.now()): EntityFilter | undefined {
  const preset = datePresets.find((candidate) => candidate.value === presetValue);
  if (!preset) return undefined;
  return { [preset.bound]: daysAgo(preset.days, now) };
}

export function datePresetOf(filter: EntityFilter | undefined, now = Date.now()): string {
  if (!filter) return anyValue;
  const preset = datePresets.find(
    (candidate) =>
      filter[candidate.bound] === daysAgo(candidate.days, now) &&
      filter[candidate.bound === "gte" ? "lte" : "gte"] === undefined,
  );
  return preset?.value ?? customValue;
}

export function DateFilter({
  label,
  filter,
  onChange,
}: Readonly<{
  label: string;
  filter: EntityFilter | undefined;
  onChange: (filter: EntityFilter | undefined) => void;
}>) {
  const selected = datePresetOf(filter);
  return (
    <Select value={selected} onValueChange={(next) => onChange(datePresetFilter(next))}>
      <SelectTrigger aria-label={label} className="bg-background">
        <span className="text-muted-foreground">{label}</span>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={anyValue}>Any</SelectItem>
        {datePresets.map((preset) => (
          <SelectItem key={preset.value} value={preset.value}>
            {preset.label}
          </SelectItem>
        ))}
        {selected === customValue ? (
          <SelectItem value={customValue} disabled>
            Custom date
          </SelectItem>
        ) : null}
      </SelectContent>
    </Select>
  );
}
